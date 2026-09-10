package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/needs"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (needs.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *needs.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *needs.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, needs.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, needs.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, needs.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/needs", h.create)
	mux.HandleFunc("GET /v1/needs", h.listOwned)
	mux.HandleFunc("GET /v1/needs/{needId}", h.getOwned)
	mux.HandleFunc("PATCH /v1/needs/{needId}", h.patch)
	mux.HandleFunc("POST /v1/needs/{needId}/open", h.open)
	mux.HandleFunc("POST /v1/needs/{needId}/fulfill", h.fulfill)
	mux.HandleFunc("POST /v1/needs/{needId}/cancel", h.cancel)
	mux.HandleFunc("GET /v1/needs/{needId}/candidates", h.listCandidates)
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
	content, ok := req.content()
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	need, err := h.svc.Create(r.Context(), ownerID, content)
	if err != nil {
		writeNeedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerDTO(need))
}

func (h *Handler) listOwned(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	list, err := h.svc.ListOwned(r.Context(), ownerID)
	if err != nil {
		writeNeedError(w, err)
		return
	}
	out := make([]ownerDTO, 0, len(list))
	for _, n := range list {
		out = append(out, toOwnerDTO(n))
	}
	writeJSON(w, http.StatusOK, listDTO{Needs: out})
}

func (h *Handler) getOwned(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	needID, ok := parseNeedID(w, r)
	if !ok {
		return
	}
	need, err := h.svc.GetOwned(r.Context(), ownerID, needID)
	if err != nil {
		writeNeedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerDTO(need))
}

func (h *Handler) patch(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	needID, ok := parseNeedID(w, r)
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
	current, err := h.svc.GetOwned(r.Context(), ownerID, needID)
	if err != nil {
		writeNeedError(w, err)
		return
	}
	content, ok := req.apply(current)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	need, err := h.svc.Update(r.Context(), ownerID, needID, content)
	if err != nil {
		writeNeedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerDTO(need))
}

func (h *Handler) open(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.svc.Open)
}

func (h *Handler) fulfill(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.svc.Fulfill)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.svc.Cancel)
}

func (h *Handler) listCandidates(w http.ResponseWriter, r *http.Request) {
	if hasClientRankingControl(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	needID, ok := parseNeedID(w, r)
	if !ok {
		return
	}
	limit, ok := parseLimit(r.URL.Query().Get("limit"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	list, err := h.svc.ListCandidates(r.Context(), ownerID, needID, limit)
	if err != nil {
		writeNeedError(w, err)
		return
	}
	out := make([]candidateDTO, 0, len(list))
	for _, c := range list {
		out = append(out, toCandidateDTO(c))
	}
	writeJSON(w, http.StatusOK, candidatesDTO{Candidates: out})
}

func (h *Handler) lifecycle(w http.ResponseWriter, r *http.Request, fn func(context.Context, needs.ID, needs.ID) (needs.Need, error)) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	needID, ok := parseNeedID(w, r)
	if !ok {
		return
	}
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	need, err := fn(r.Context(), ownerID, needID)
	if err != nil {
		writeNeedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerDTO(need))
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

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (needs.ID, bool) {
	if !h.requireOrigin(w, r) {
		return needs.ID{}, false
	}
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return needs.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return needs.ID{}, false
	}
	return ownerID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (needs.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return needs.ID{}, false
	}
	ownerID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return needs.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return needs.ID{}, false
	}
	if ownerID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return needs.ID{}, false
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

func parseNeedID(w http.ResponseWriter, r *http.Request) (needs.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("needId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return needs.ID{}, false
	}
	id, err := needs.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return needs.ID{}, false
	}
	return id, true
}

type spoofFields struct {
	RequesterUserID      string          `json:"requesterUserId"`
	RequesterUserIDSnake string          `json:"requester_user_id"`
	OwnerUserID          string          `json:"ownerUserId"`
	UserID               string          `json:"userId"`
	Status               string          `json:"status"`
	NeedID               string          `json:"needId"`
	ID                   string          `json:"id"`
	CreatedAt            string          `json:"createdAt"`
	UpdatedAt            string          `json:"updatedAt"`
	Match                json.RawMessage `json:"match"`
	Offer                json.RawMessage `json:"offer"`
	ProviderID           string          `json:"providerId"`
}

func spoofedFields(s spoofFields) bool {
	return strings.TrimSpace(s.RequesterUserID) != "" ||
		strings.TrimSpace(s.RequesterUserIDSnake) != "" ||
		strings.TrimSpace(s.OwnerUserID) != "" ||
		strings.TrimSpace(s.UserID) != "" ||
		strings.TrimSpace(s.Status) != "" ||
		strings.TrimSpace(s.NeedID) != "" ||
		strings.TrimSpace(s.ID) != "" ||
		strings.TrimSpace(s.CreatedAt) != "" ||
		strings.TrimSpace(s.UpdatedAt) != "" ||
		len(s.Match) > 0 ||
		len(s.Offer) > 0 ||
		strings.TrimSpace(s.ProviderID) != ""
}

type locationBody struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

type budgetBody struct {
	MinAmount *string `json:"minAmount"`
	MaxAmount *string `json:"maxAmount"`
	Currency  *string `json:"currency"`
}

type createRequest struct {
	Title       string        `json:"title"`
	Description *string       `json:"description"`
	CategoryID  *string       `json:"categoryId"`
	Budget      *budgetBody   `json:"budget"`
	Location    *locationBody `json:"location"`
	RadiusKm    *float64      `json:"radiusKm"`
	ExpiresAt   *string       `json:"expiresAt"`
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
	*c = createRequest(raw)
	c.Spoof = spoof
	return nil
}

func (c createRequest) content() (needs.Content, bool) {
	if c.Location == nil || c.Location.Latitude == nil || c.Location.Longitude == nil {
		return needs.Content{}, false
	}
	content := needs.Content{
		Title:       c.Title,
		Description: deref(c.Description),
		Location:    needs.Coordinates{Latitude: *c.Location.Latitude, Longitude: *c.Location.Longitude},
		RadiusKm:    c.RadiusKm,
	}
	cat, ok := parseOptionalID(c.CategoryID)
	if !ok {
		return needs.Content{}, false
	}
	content.CategoryID = cat
	budget, ok := parseBudget(c.Budget)
	if !ok {
		return needs.Content{}, false
	}
	content.Budget = budget
	expires, ok := parseExpires(c.ExpiresAt)
	if !ok {
		return needs.Content{}, false
	}
	content.ExpiresAt = expires
	return content, true
}

type patchRequest struct {
	Title       *string       `json:"title"`
	Description *string       `json:"description"`
	CategoryID  *string       `json:"categoryId"`
	Budget      *budgetBody   `json:"budget"`
	Location    *locationBody `json:"location"`
	RadiusKm    *float64      `json:"radiusKm"`
	ExpiresAt   *string       `json:"expiresAt"`
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
	*c = patchRequest(raw)
	c.Spoof = spoof
	return nil
}

func (c patchRequest) apply(current needs.Need) (needs.Content, bool) {
	content := needs.Content{
		Title:       current.Title,
		Description: current.Description,
		CategoryID:  current.CategoryID,
		Budget:      current.Budget,
		Location:    needs.Coordinates{Latitude: current.Latitude, Longitude: current.Longitude},
		RadiusKm:    current.RadiusKm,
		ExpiresAt:   current.ExpiresAt,
	}
	if c.Title != nil {
		content.Title = *c.Title
	}
	if c.Description != nil {
		content.Description = *c.Description
	}
	if c.CategoryID != nil {
		if strings.TrimSpace(*c.CategoryID) == "" {
			content.CategoryID = nil
		} else {
			id, err := needs.ParseID(*c.CategoryID)
			if err != nil {
				return needs.Content{}, false
			}
			content.CategoryID = &id
		}
	}
	if c.Budget != nil {
		budget, ok := parseBudget(c.Budget)
		if !ok {
			return needs.Content{}, false
		}
		content.Budget = budget
	}
	if c.Location != nil {
		if c.Location.Latitude == nil || c.Location.Longitude == nil {
			return needs.Content{}, false
		}
		content.Location = needs.Coordinates{Latitude: *c.Location.Latitude, Longitude: *c.Location.Longitude}
	}
	if c.RadiusKm != nil {
		content.RadiusKm = c.RadiusKm
	}
	if c.ExpiresAt != nil {
		expires, ok := parseExpires(c.ExpiresAt)
		if !ok {
			return needs.Content{}, false
		}
		content.ExpiresAt = expires
	}
	return content, true
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
	NeedID      string      `json:"needId"`
	Title       string      `json:"title"`
	Description *string     `json:"description,omitempty"`
	CategoryID  *string     `json:"categoryId,omitempty"`
	Status      string      `json:"status"`
	Budget      *budgetDTO  `json:"budget,omitempty"`
	Location    locationDTO `json:"location"`
	RadiusKm    *float64    `json:"radiusKm,omitempty"`
	CreatedAt   string      `json:"createdAt"`
	UpdatedAt   string      `json:"updatedAt"`
	ExpiresAt   *string     `json:"expiresAt,omitempty"`
}

type budgetDTO struct {
	MinAmount string `json:"minAmount"`
	MaxAmount string `json:"maxAmount"`
	Currency  string `json:"currency"`
}

type locationDTO struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type listDTO struct {
	Needs []ownerDTO `json:"needs"`
}

type candidatesDTO struct {
	Candidates []candidateDTO `json:"candidates"`
}

type candidateDTO struct {
	BusinessID          string  `json:"businessId"`
	BusinessDisplayName string  `json:"businessDisplayName"`
	ServiceID           string  `json:"serviceId"`
	ServiceTitle        string  `json:"serviceTitle"`
	ServiceDescription  *string `json:"serviceDescription,omitempty"`
	PriceModel          *string `json:"priceModel,omitempty"`
	Amount              *string `json:"amount,omitempty"`
	Currency            *string `json:"currency,omitempty"`
	DistanceKm          float64 `json:"distanceKm"`
	CategoryID          *string `json:"categoryId,omitempty"`
}

func toCandidateDTO(c needs.ServiceCandidate) candidateDTO {
	dto := candidateDTO{
		BusinessID:          c.BusinessID.String(),
		BusinessDisplayName: c.BusinessDisplayName,
		ServiceID:           c.ServiceID.String(),
		ServiceTitle:        c.ServiceTitle,
		ServiceDescription:  optionalString(c.ServiceDescription),
		PriceModel:          optionalString(c.PriceModel),
		Amount:              c.PriceAmount,
		Currency:            c.PriceCurrency,
		DistanceKm:          c.DistanceKm,
	}
	if c.CategoryID != nil {
		s := c.CategoryID.String()
		dto.CategoryID = &s
	}
	return dto
}

func parseLimit(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
		if n > needs.MaxCandidateLimit*10 {
			return 0, false
		}
	}
	return n, true
}

func hasClientRankingControl(r *http.Request) bool {
	q := r.URL.Query()
	return hasForeignUserID(r) ||
		strings.TrimSpace(q.Get("radiusKm")) != "" ||
		strings.TrimSpace(q.Get("radius")) != "" ||
		strings.TrimSpace(q.Get("sort")) != "" ||
		strings.TrimSpace(q.Get("order")) != "" ||
		strings.TrimSpace(q.Get("score")) != "" ||
		strings.TrimSpace(q.Get("trust")) != "" ||
		strings.TrimSpace(q.Get("offset")) != "" ||
		strings.TrimSpace(q.Get("cursor")) != ""
}

type errorResponse struct {
	Error string `json:"error"`
}

func toOwnerDTO(n needs.Need) ownerDTO {
	dto := ownerDTO{
		NeedID: n.ID.String(),
		Title:  n.Title,
		Status: string(n.Status),
		Location: locationDTO{
			Latitude:  n.Latitude,
			Longitude: n.Longitude,
		},
		RadiusKm:  n.RadiusKm,
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: n.UpdatedAt.UTC().Format(time.RFC3339),
	}
	dto.Description = optionalString(n.Description)
	if n.CategoryID != nil {
		s := n.CategoryID.String()
		dto.CategoryID = &s
	}
	if n.Budget != nil {
		dto.Budget = &budgetDTO{
			MinAmount: n.Budget.MinAmount,
			MaxAmount: n.Budget.MaxAmount,
			Currency:  n.Budget.Currency,
		}
	}
	if n.ExpiresAt != nil {
		s := n.ExpiresAt.UTC().Format(time.RFC3339)
		dto.ExpiresAt = &s
	}
	return dto
}

func parseOptionalID(raw *string) (*needs.ID, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, true
	}
	id, err := needs.ParseID(*raw)
	if err != nil {
		return nil, false
	}
	return &id, true
}

func parseBudget(b *budgetBody) (*needs.Budget, bool) {
	if b == nil {
		return nil, true
	}
	minAmt := deref(b.MinAmount)
	maxAmt := deref(b.MaxAmount)
	cur := deref(b.Currency)
	if minAmt == "" && maxAmt == "" && cur == "" {
		return nil, true
	}
	return &needs.Budget{MinAmount: minAmt, MaxAmount: maxAmt, Currency: cur}, true
}

func parseExpires(raw *string) (*time.Time, bool) {
	if raw == nil {
		return nil, true
	}
	s := strings.TrimSpace(*raw)
	if s == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, false
	}
	return &t, true
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

func writeNeedError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, needs.ErrInvalidNeed), errors.Is(err, needs.ErrInvalidContent),
		errors.Is(err, needs.ErrInvalidStatus), errors.Is(err, needs.ErrZeroID),
		errors.Is(err, needs.ErrInvalidLocation), errors.Is(err, needs.ErrInvalidLatitude),
		errors.Is(err, needs.ErrInvalidLongitude), errors.Is(err, needs.ErrInvalidBudget),
		errors.Is(err, needs.ErrInvalidRadius), errors.Is(err, needs.ErrInvalidExpiry),
		errors.Is(err, needs.ErrInvalidCategory):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, needs.ErrNotFound), errors.Is(err, needs.ErrForbidden):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, needs.ErrConflict), errors.Is(err, needs.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, needs.ErrUnavailable), errors.Is(err, needs.ErrStoreRequired):
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
		strings.TrimSpace(q.Get("requesterUserId")) != "" || strings.TrimSpace(q.Get("requester_user_id")) != "" ||
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
