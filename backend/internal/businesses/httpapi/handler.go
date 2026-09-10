package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/businesses"
)

const (
	sessionCookieName  = "__Host-konumlu_session"
	csrfCookieName     = "__Host-konumlu_csrf"
	csrfHeaderName     = "X-CSRF-Token"
	publicCacheControl = "public, max-age=30"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (businesses.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *businesses.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *businesses.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, businesses.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, businesses.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, businesses.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/businesses", h.create)
	mux.HandleFunc("GET /v1/businesses/mine", h.getMine)
	mux.HandleFunc("GET /v1/businesses/{businessId}", h.getOwned)
	mux.HandleFunc("PATCH /v1/businesses/{businessId}", h.patch)
	mux.HandleFunc("POST /v1/businesses/{businessId}/activate", h.activate)
	mux.HandleFunc("POST /v1/businesses/{businessId}/close", h.close)
	mux.HandleFunc("POST /v1/businesses/{businessId}/services", h.createService)
	mux.HandleFunc("GET /v1/businesses/{businessId}/services", h.listOwnedServices)
	mux.HandleFunc("GET /v1/businesses/{businessId}/services/{serviceId}", h.getOwnedService)
	mux.HandleFunc("PATCH /v1/businesses/{businessId}/services/{serviceId}", h.patchService)
	mux.HandleFunc("POST /v1/businesses/{businessId}/services/{serviceId}/activate", h.activateService)
	mux.HandleFunc("POST /v1/businesses/{businessId}/services/{serviceId}/pause", h.pauseService)
	mux.HandleFunc("POST /v1/businesses/{businessId}/services/{serviceId}/close", h.closeService)
	mux.HandleFunc("GET /v1/public/businesses/{businessId}", h.getPublic)
	mux.HandleFunc("GET /v1/public/businesses/{businessId}/services", h.listPublicServices)
	mux.HandleFunc("GET /v1/public/services/{serviceId}", h.getPublicService)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if spoofedFields(req.Spoof) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	profile, err := h.svc.Create(r.Context(), ownerID, businesses.ProfileContent{
		DisplayName: req.DisplayName,
		Description: deref(req.Description),
	})
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	if req.hasLocation {
		profile, err = h.svc.UpdateLocation(r.Context(), ownerID, profile.ID, req.location())
		if err != nil {
			writeBusinessError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, toOwnerDTO(profile))
}

func (h *Handler) getMine(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	profile, err := h.svc.GetMine(r.Context(), ownerID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerDTO(profile))
}

func (h *Handler) getOwned(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	profile, err := h.svc.GetOwned(r.Context(), ownerID, businessID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerDTO(profile))
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	var req patchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if spoofedFields(req.Spoof) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	current, err := h.svc.GetOwned(r.Context(), ownerID, businessID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	content := businesses.ProfileContent{
		DisplayName: current.DisplayName,
		Description: current.Description,
	}
	if req.DisplayName != nil {
		content.DisplayName = *req.DisplayName
	}
	if req.Description != nil {
		content.Description = *req.Description
	}
	profile, err := h.svc.Update(r.Context(), ownerID, businessID, content)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	if req.hasLocation {
		profile, err = h.svc.UpdateLocation(r.Context(), ownerID, businessID, req.location())
		if err != nil {
			writeBusinessError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, toOwnerDTO(profile))
}

func (h *Handler) activate(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	profile, err := h.svc.Activate(r.Context(), ownerID, businessID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerDTO(profile))
}

func (h *Handler) close(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	profile, err := h.svc.Close(r.Context(), ownerID, businessID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerDTO(profile))
}

func (h *Handler) getPublic(w http.ResponseWriter, r *http.Request) {
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	profile, err := h.svc.GetPublic(r.Context(), businessID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", publicCacheControl)
	writeJSON(w, http.StatusOK, toPublicDTO(profile))
}

func (h *Handler) rejectMutationSpoof(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	var req mutationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	if spoofedFields(req.Spoof) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (businesses.ID, bool) {
	if !h.requireOrigin(w, r) {
		return businesses.ID{}, false
	}
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return businesses.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return businesses.ID{}, false
	}
	return ownerID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (businesses.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return businesses.ID{}, false
	}
	ownerID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return businesses.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return businesses.ID{}, false
	}
	if ownerID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return businesses.ID{}, false
	}
	return ownerID, true
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

func parseBusinessID(w http.ResponseWriter, r *http.Request) (businesses.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("businessId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return businesses.ID{}, false
	}
	id, err := businesses.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return businesses.ID{}, false
	}
	return id, true
}

type spoofFields struct {
	OwnerUserID          string          `json:"ownerUserId"`
	OwnerUserIDSnake     string          `json:"owner_user_id"`
	Status               string          `json:"status"`
	BusinessID           string          `json:"businessId"`
	ID                   string          `json:"id"`
	ServiceID            string          `json:"serviceId"`
	ServiceIDSnake       string          `json:"service_id"`
	Slug                 string          `json:"slug"`
	CreatedAt            string          `json:"createdAt"`
	UpdatedAt            string          `json:"updatedAt"`
	VerificationStatus   string          `json:"verificationStatus"`
	Verified             json.RawMessage `json:"verified"`
	Phone                string          `json:"phone"`
	Email                string          `json:"email"`
	ModerationState      json.RawMessage `json:"moderationState"`
	ModerationStateSnake json.RawMessage `json:"moderation_state"`
}

func spoofedFields(s spoofFields) bool {
	return strings.TrimSpace(s.OwnerUserID) != "" ||
		strings.TrimSpace(s.OwnerUserIDSnake) != "" ||
		strings.TrimSpace(s.Status) != "" ||
		strings.TrimSpace(s.BusinessID) != "" ||
		strings.TrimSpace(s.ID) != "" ||
		strings.TrimSpace(s.ServiceID) != "" ||
		strings.TrimSpace(s.ServiceIDSnake) != "" ||
		strings.TrimSpace(s.Slug) != "" ||
		strings.TrimSpace(s.CreatedAt) != "" ||
		strings.TrimSpace(s.UpdatedAt) != "" ||
		strings.TrimSpace(s.VerificationStatus) != "" ||
		len(s.Verified) > 0 ||
		strings.TrimSpace(s.Phone) != "" ||
		strings.TrimSpace(s.Email) != "" ||
		len(s.ModerationState) > 0 ||
		len(s.ModerationStateSnake) > 0
}

type locationBody struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type createRequest struct {
	DisplayName string        `json:"displayName"`
	Description *string       `json:"description"`
	Location    *locationBody `json:"location"`
	hasLocation bool
	Spoof       spoofFields
}

func (c *createRequest) UnmarshalJSON(b []byte) error {
	type alias createRequest
	var raw alias
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var spoof spoofFields
	if err := json.Unmarshal(b, &spoof); err != nil {
		return err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return err
	}
	*c = createRequest(raw)
	c.Spoof = spoof
	_, c.hasLocation = keys["location"]
	return nil
}

func (c createRequest) location() *businesses.Coordinates {
	return parseLocation(c.Location)
}

type patchRequest struct {
	DisplayName *string       `json:"displayName"`
	Description *string       `json:"description"`
	Location    *locationBody `json:"location"`
	hasLocation bool
	Spoof       spoofFields
}

func (c *patchRequest) UnmarshalJSON(b []byte) error {
	type alias patchRequest
	var raw alias
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var spoof spoofFields
	if err := json.Unmarshal(b, &spoof); err != nil {
		return err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return err
	}
	*c = patchRequest(raw)
	c.Spoof = spoof
	_, c.hasLocation = keys["location"]
	return nil
}

func (c patchRequest) location() *businesses.Coordinates {
	return parseLocation(c.Location)
}

type mutationRequest struct {
	Spoof spoofFields
}

func (c *mutationRequest) UnmarshalJSON(b []byte) error {
	var spoof spoofFields
	if err := json.Unmarshal(b, &spoof); err != nil {
		return err
	}
	c.Spoof = spoof
	return nil
}

type ownerDTO struct {
	BusinessID  string       `json:"businessId"`
	DisplayName string       `json:"displayName"`
	Description *string      `json:"description,omitempty"`
	Status      string       `json:"status"`
	Location    *locationDTO `json:"location,omitempty"`
	CreatedAt   string       `json:"createdAt"`
	UpdatedAt   string       `json:"updatedAt"`
}

type publicDTO struct {
	BusinessID  string       `json:"businessId"`
	DisplayName string       `json:"displayName"`
	Description *string      `json:"description,omitempty"`
	Location    *locationDTO `json:"location,omitempty"`
	CreatedAt   string       `json:"createdAt"`
	UpdatedAt   string       `json:"updatedAt"`
}

type locationDTO struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toOwnerDTO(p businesses.Profile) ownerDTO {
	return ownerDTO{
		BusinessID:  p.ID.String(),
		DisplayName: p.DisplayName,
		Description: optionalString(p.Description),
		Status:      string(p.Status),
		Location:    toLocationDTO(p.Location),
		CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toPublicDTO(p businesses.Profile) publicDTO {
	return publicDTO{
		BusinessID:  p.ID.String(),
		DisplayName: p.DisplayName,
		Description: optionalString(p.Description),
		Location:    toLocationDTO(p.Location),
		CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toLocationDTO(c *businesses.Coordinates) *locationDTO {
	if c == nil {
		return nil
	}
	return &locationDTO{Latitude: c.Latitude, Longitude: c.Longitude}
}

func parseLocation(body *locationBody) *businesses.Coordinates {
	if body == nil || body.Latitude == nil || body.Longitude == nil {
		return nil
	}
	return &businesses.Coordinates{Latitude: *body.Latitude, Longitude: *body.Longitude}
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	out := s
	return &out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func writeBusinessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, businesses.ErrInvalidBusiness), errors.Is(err, businesses.ErrInvalidContent),
		errors.Is(err, businesses.ErrInvalidStatus), errors.Is(err, businesses.ErrZeroID),
		errors.Is(err, businesses.ErrInvalidOfferedService), errors.Is(err, businesses.ErrInvalidPrice),
		errors.Is(err, businesses.ErrInvalidLocation), errors.Is(err, businesses.ErrInvalidLatitude),
		errors.Is(err, businesses.ErrInvalidLongitude), errors.Is(err, businesses.ErrInvalidCategory),
		errors.Is(err, businesses.ErrInvalidRadius):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, businesses.ErrNotFound), errors.Is(err, businesses.ErrForbidden):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, businesses.ErrConflict), errors.Is(err, businesses.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, businesses.ErrUnavailable), errors.Is(err, businesses.ErrStoreRequired):
		writeError(w, http.StatusServiceUnavailable, "unavailable")
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

func decodeJSON(r *http.Request, dest any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dest); err != nil {
		return err
	}
	return nil
}

func hasForeignUserID(r *http.Request) bool {
	q := r.URL.Query()
	return strings.TrimSpace(q.Get("userId")) != "" || strings.TrimSpace(q.Get("user_id")) != "" ||
		strings.TrimSpace(q.Get("ownerUserId")) != "" || strings.TrimSpace(q.Get("owner_user_id")) != ""
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
