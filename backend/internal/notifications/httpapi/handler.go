package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/notifications"
	"backend/internal/notifications/policy"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
	maxJSONBytes      = 16 << 10
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (notifications.ID, error)
}

type consumerAPI interface {
	GetPreferences(ctx context.Context, userID notifications.ID) ([]policy.EffectivePreference, error)
	PatchPreferences(ctx context.Context, userID notifications.ID, patch policy.PreferencePatch) ([]policy.EffectivePreference, error)
	GetConsents(ctx context.Context, userID notifications.ID) ([]notifications.ConsentView, error)
	RecordConsent(ctx context.Context, userID notifications.ID, typ policy.ConsentType, grant bool) (policy.ConsentSnapshot, error)
	ListInbox(ctx context.Context, userID notifications.ID, cursorRaw string, limit int) (notifications.InboxPage, error)
	MarkRead(ctx context.Context, userID, inboxID notifications.ID) (notifications.InboxRow, error)
	MarkAllRead(ctx context.Context, userID notifications.ID) error
	RegisterPushEndpoint(ctx context.Context, userID notifications.ID, in notifications.PushRegistration) (notifications.PushEndpointView, error)
	ListPushEndpoints(ctx context.Context, userID notifications.ID) ([]notifications.PushEndpointView, error)
	RevokePushEndpoint(ctx context.Context, userID, endpointID notifications.ID) (notifications.PushEndpointView, error)
}

type Handler struct {
	sessions sessionResolver
	svc      consumerAPI
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc consumerAPI, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, notifications.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, notifications.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, notifications.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/notification-preferences", h.getPreferences)
	mux.HandleFunc("PATCH /v1/notification-preferences", h.patchPreferences)
	mux.HandleFunc("GET /v1/notification-consents", h.getConsents)
	mux.HandleFunc("POST /v1/notification-consents", h.postConsent)
	mux.HandleFunc("GET /v1/notifications", h.listInbox)
	mux.HandleFunc("POST /v1/notifications/read-all", h.markAllRead)
	mux.HandleFunc("POST /v1/notifications/{inboxId}/read", h.markRead)
	mux.HandleFunc("POST /v1/push-endpoints", h.registerPush)
	mux.HandleFunc("GET /v1/push-endpoints", h.listPush)
	mux.HandleFunc("DELETE /v1/push-endpoints/{endpointId}", h.revokePush)
}

func (h *Handler) getPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.GetPreferences(r.Context(), userID)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preferenceListDTO{CatalogVersion: policy.CatalogVersion, Settings: toPrefDTOs(rows)})
}

func (h *Handler) patchPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	var req preferencePatchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	patch, err := req.toPatch()
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	rows, err := h.svc.PatchPreferences(r.Context(), userID, patch)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preferenceListDTO{CatalogVersion: policy.CatalogVersion, Settings: toPrefDTOs(rows)})
}

func (h *Handler) getConsents(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.GetConsents(r.Context(), userID)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	out := make([]consentDTO, 0, len(rows))
	for _, row := range rows {
		dto := consentDTO{ConsentType: string(row.Type), PolicyVersion: row.PolicyVersion}
		if row.Decision != nil {
			s := string(*row.Decision)
			dto.Decision = &s
		}
		if row.RecordedAt != nil {
			t := row.RecordedAt.UTC().Format(time.RFC3339)
			dto.RecordedAt = &t
		}
		if row.Source != "" {
			s := string(row.Source)
			dto.Source = &s
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, consentListDTO{Consents: out})
}

func (h *Handler) postConsent(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	var req consentPostRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	grant, err := parseConsentDecision(req.Decision)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	typ := policy.ConsentType(strings.TrimSpace(req.ConsentType))
	snap, err := h.svc.RecordConsent(r.Context(), userID, typ, grant)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	decision := string(snap.Status)
	recorded := snap.CapturedAt.UTC().Format(time.RFC3339)
	source := string(snap.Source)
	writeJSON(w, http.StatusOK, consentDTO{
		ConsentType:   string(snap.Type),
		Decision:      &decision,
		PolicyVersion: snap.PolicyVersion,
		RecordedAt:    &recorded,
		Source:        &source,
	})
}

func (h *Handler) listInbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	limit := 0
	if r.URL.Query().Has("limit") {
		n, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		limit = n
	}
	page, err := h.svc.ListInbox(r.Context(), userID, strings.TrimSpace(r.URL.Query().Get("cursor")), limit)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	items := make([]inboxItemDTO, 0, len(page.Items))
	for _, row := range page.Items {
		items = append(items, toInboxDTO(row))
	}
	writeJSON(w, http.StatusOK, inboxListDTO{Items: items, NextCursor: omitEmpty(page.NextCursor)})
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	inboxID, ok := parseInboxID(w, r)
	if !ok {
		return
	}
	row, err := h.svc.MarkRead(r.Context(), userID, inboxID)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toInboxDTO(row))
}

func (h *Handler) markAllRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	if err := h.svc.MarkAllRead(r.Context(), userID); err != nil {
		writeNotifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, markAllDTO{OK: true})
}

func (h *Handler) registerPush(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	var req pushRegisterRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := req.toRegistration()
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	view, err := h.svc.RegisterPushEndpoint(r.Context(), userID, in)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPushDTO(view))
}

func (h *Handler) listPush(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.ListPushEndpoints(r.Context(), userID)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	out := make([]pushEndpointDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toPushDTO(row))
	}
	writeJSON(w, http.StatusOK, pushListDTO{Endpoints: out})
}

func (h *Handler) revokePush(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	endpointID, ok := parseEndpointID(w, r)
	if !ok {
		return
	}
	view, err := h.svc.RevokePushEndpoint(r.Context(), userID, endpointID)
	if err != nil {
		writeNotifyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPushDTO(view))
}

type preferencePatchRequest struct {
	Overrides []preferenceOverrideDTO `json:"overrides"`
}

type preferenceOverrideDTO struct {
	Channel   string `json:"channel"`
	ScopeType string `json:"scopeType"`
	ScopeKey  string `json:"scopeKey"`
	Enabled   bool   `json:"enabled"`
}

func (r preferencePatchRequest) toPatch() (policy.PreferencePatch, error) {
	out := policy.PreferencePatch{Overrides: make([]policy.PreferenceOverride, 0, len(r.Overrides))}
	for _, o := range r.Overrides {
		out.Overrides = append(out.Overrides, policy.PreferenceOverride{
			Channel:   policy.Channel(strings.TrimSpace(o.Channel)),
			ScopeType: policy.ScopeType(strings.TrimSpace(o.ScopeType)),
			ScopeKey:  strings.TrimSpace(o.ScopeKey),
			Enabled:   o.Enabled,
		})
	}
	if err := policy.ValidatePreferencePatch(out); err != nil {
		return policy.PreferencePatch{}, err
	}
	return out, nil
}

type preferenceDTO struct {
	Channel   string `json:"channel"`
	ScopeType string `json:"scopeType"`
	ScopeKey  string `json:"scopeKey"`
	Stored    *bool  `json:"stored"`
	Enabled   bool   `json:"enabled"`
	Required  bool   `json:"required"`
}

type preferenceListDTO struct {
	CatalogVersion int             `json:"catalogVersion"`
	Settings       []preferenceDTO `json:"settings"`
}

func toPrefDTOs(rows []policy.EffectivePreference) []preferenceDTO {
	out := make([]preferenceDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, preferenceDTO{
			Channel:   string(row.Channel),
			ScopeType: string(row.ScopeType),
			ScopeKey:  row.ScopeKey,
			Stored:    row.Stored,
			Enabled:   row.Enabled,
			Required:  row.Required,
		})
	}
	return out
}

type consentPostRequest struct {
	ConsentType string `json:"consentType"`
	Decision    string `json:"decision"`
}

type consentDTO struct {
	ConsentType   string  `json:"consentType"`
	Decision      *string `json:"decision"`
	PolicyVersion string  `json:"policyVersion"`
	RecordedAt    *string `json:"recordedAt,omitempty"`
	Source        *string `json:"source,omitempty"`
}

type consentListDTO struct {
	Consents []consentDTO `json:"consents"`
}

type inboxItemDTO struct {
	ID          string            `json:"id"`
	EventType   string            `json:"eventType"`
	Purpose     string            `json:"purpose"`
	TemplateKey string            `json:"templateKey"`
	Variables   map[string]string `json:"variables"`
	ResourceRef string            `json:"resourceRef,omitempty"`
	CreatedAt   string            `json:"createdAt"`
	ReadAt      *string           `json:"readAt,omitempty"`
}

type inboxListDTO struct {
	Items      []inboxItemDTO `json:"items"`
	NextCursor *string        `json:"nextCursor,omitempty"`
}

type markAllDTO struct {
	OK bool `json:"ok"`
}

type pushRegisterRequest struct {
	Channel  string `json:"channel"`
	Platform string `json:"platform"`
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint,omitempty"`
	P256dh   string `json:"p256dh,omitempty"`
	Auth     string `json:"auth,omitempty"`
	Token    string `json:"token,omitempty"`
}

func (r pushRegisterRequest) toRegistration() (notifications.PushRegistration, error) {
	in := notifications.PushRegistration{
		Channel:  policy.Channel(strings.TrimSpace(r.Channel)),
		Platform: notifications.PushPlatform(strings.TrimSpace(r.Platform)),
		Provider: notifications.PushProvider(strings.TrimSpace(r.Provider)),
	}
	switch in.Channel {
	case policy.ChannelWebPush:
		in.Web = &notifications.WebPushMaterial{Endpoint: r.Endpoint, P256dh: r.P256dh, Auth: r.Auth}
	case policy.ChannelMobilePush:
		in.Mobile = &notifications.MobilePushMaterial{Token: r.Token}
	}
	if err := notifications.ValidatePushRegistration(in); err != nil {
		return notifications.PushRegistration{}, err
	}
	return in, nil
}

type pushEndpointDTO struct {
	ID         string `json:"id"`
	Channel    string `json:"channel"`
	Platform   string `json:"platform"`
	Provider   string `json:"provider"`
	CreatedAt  string `json:"createdAt"`
	LastSeenAt string `json:"lastSeenAt"`
	Revoked    bool   `json:"revoked"`
}

type pushListDTO struct {
	Endpoints []pushEndpointDTO `json:"endpoints"`
}

func toPushDTO(row notifications.PushEndpointView) pushEndpointDTO {
	return pushEndpointDTO{
		ID:         row.ID.String(),
		Channel:    string(row.Channel),
		Platform:   string(row.Platform),
		Provider:   string(row.Provider),
		CreatedAt:  row.CreatedAt.UTC().Format(time.RFC3339),
		LastSeenAt: row.LastSeenAt.UTC().Format(time.RFC3339),
		Revoked:    row.Revoked,
	}
}

func parseEndpointID(w http.ResponseWriter, r *http.Request) (notifications.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("endpointId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusNotFound, "not_found")
		return notifications.ID{}, false
	}
	id, err := notifications.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return notifications.ID{}, false
	}
	return id, true
}

type errorResponse struct {
	Error string `json:"error"`
}

func toInboxDTO(row notifications.InboxRow) inboxItemDTO {
	dto := inboxItemDTO{
		ID:          row.ID.String(),
		EventType:   string(row.EventType),
		Purpose:     string(row.Purpose),
		TemplateKey: row.TemplateKey,
		Variables:   row.Variables,
		ResourceRef: row.ResourceRef,
		CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
	}
	if dto.Variables == nil {
		dto.Variables = map[string]string{}
	}
	if row.ReadAt != nil {
		t := row.ReadAt.UTC().Format(time.RFC3339)
		dto.ReadAt = &t
	}
	return dto
}

func parseConsentDecision(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "granted":
		return true, nil
	case "withdrawn":
		return false, nil
	default:
		return false, policy.ErrInvalidInput
	}
}

func parseInboxID(w http.ResponseWriter, r *http.Request) (notifications.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("inboxId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusNotFound, "not_found")
		return notifications.ID{}, false
	}
	id, err := notifications.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return notifications.ID{}, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	limited := http.MaxBytesReader(w, r.Body, maxJSONBytes)
	dec := json.NewDecoder(limited)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (notifications.ID, bool) {
	if !h.requireOrigin(w, r) {
		return notifications.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return notifications.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return notifications.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (notifications.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return notifications.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return notifications.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return notifications.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return notifications.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || origin == "null" {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if _, ok := h.origins[origin]; !ok {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func writeNotifyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, policy.ErrUnknownPreference), errors.Is(err, policy.ErrUnknownConsent),
		errors.Is(err, policy.ErrInvalidInput), errors.Is(err, policy.ErrArbitraryMetadata),
		errors.Is(err, policy.ErrClientTimestamp), errors.Is(err, notifications.ErrInvalidQuery):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, policy.ErrSystemRequired):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, policy.ErrNotFound), errors.Is(err, notifications.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, notifications.ErrEndpointConflict), errors.Is(err, notifications.ErrConflict):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, policy.ErrInvalidActor):
		writeError(w, http.StatusUnauthorized, "unauthenticated")
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorResponse{Error: code})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func readCookie(r *http.Request, name string) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil || c == nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

func csrfOK(r *http.Request) bool {
	cookie, ok := readCookie(r, csrfCookieName)
	if !ok {
		return false
	}
	header := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	if header == "" || len(header) != len(cookie) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(cookie)) == 1
}

func omitEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
