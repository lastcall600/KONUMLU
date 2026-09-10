package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/savedsearch"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (savedsearch.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *savedsearch.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *savedsearch.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, savedsearch.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, savedsearch.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, savedsearch.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/saved-searches", h.create)
	mux.HandleFunc("GET /v1/saved-searches", h.list)
	mux.HandleFunc("GET /v1/saved-searches/{id}", h.get)
	mux.HandleFunc("DELETE /v1/saved-searches/{id}", h.remove)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	in, ok := parseCreate(w, r)
	if !ok {
		return
	}
	row, err := h.svc.Create(r.Context(), userID, in)
	if err != nil {
		writeSavedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(row))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.List(r.Context(), userID)
	if err != nil {
		writeSavedError(w, err)
		return
	}
	out := make([]savedSearchDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDTO(row))
	}
	writeJSON(w, http.StatusOK, listDTO{SavedSearches: out})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	row, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		writeSavedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(row))
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		writeSavedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, deleteDTO{ID: id.String()})
}

func parsePathID(w http.ResponseWriter, r *http.Request) (savedsearch.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("id"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return savedsearch.ID{}, false
	}
	id, err := savedsearch.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return savedsearch.ID{}, false
	}
	return id, true
}

func parseCreate(w http.ResponseWriter, r *http.Request) (savedsearch.CreateInput, bool) {
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return savedsearch.CreateInput{}, false
	}
	if req.Cursor != nil || req.UserID != nil || req.UserIDSnake != nil || req.ID != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return savedsearch.CreateInput{}, false
	}
	in := savedsearch.CreateInput{Name: req.Name}
	if req.Q != nil {
		q := strings.TrimSpace(*req.Q)
		in.Filters.Q = &q
	}
	if req.CategoryID != nil {
		raw := strings.TrimSpace(*req.CategoryID)
		if raw != "" {
			id, err := savedsearch.ParseID(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_request")
				return savedsearch.CreateInput{}, false
			}
			in.Filters.CategoryID = &id
		}
	}
	minP, err := optionalPrice(req.MinPrice)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return savedsearch.CreateInput{}, false
	}
	in.Filters.MinPrice = minP
	maxP, err := optionalPrice(req.MaxPrice)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return savedsearch.CreateInput{}, false
	}
	in.Filters.MaxPrice = maxP
	if req.Currency != nil {
		cur := strings.TrimSpace(*req.Currency)
		in.Filters.Currency = &cur
	}
	geoCount := countPresent(req.North, req.South, req.East, req.West)
	if geoCount != 0 && geoCount != 4 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return savedsearch.CreateInput{}, false
	}
	if geoCount == 4 {
		in.Filters.Viewport = &savedsearch.Viewport{
			North: *req.North,
			South: *req.South,
			East:  *req.East,
			West:  *req.West,
		}
	}
	return in, true
}

func optionalPrice(raw *flexibleString) (*string, error) {
	if raw == nil || !raw.set {
		return nil, nil
	}
	s := strings.TrimSpace(raw.val)
	return &s, nil
}

func countPresent(vals ...*float64) int {
	n := 0
	for _, v := range vals {
		if v != nil {
			n++
		}
	}
	return n
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (savedsearch.ID, bool) {
	if !h.requireOrigin(w, r) {
		return savedsearch.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return savedsearch.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return savedsearch.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (savedsearch.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return savedsearch.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return savedsearch.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return savedsearch.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return savedsearch.ID{}, false
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

type createRequest struct {
	Name       string          `json:"name"`
	Q          *string         `json:"q"`
	CategoryID *string         `json:"categoryId"`
	MinPrice   *flexibleString `json:"minPrice"`
	MaxPrice   *flexibleString `json:"maxPrice"`
	Currency   *string         `json:"currency"`
	North      *float64        `json:"north"`
	South      *float64        `json:"south"`
	East       *float64        `json:"east"`
	West       *float64        `json:"west"`
	Cursor      json.RawMessage `json:"cursor"`
	UserID      json.RawMessage `json:"userId"`
	UserIDSnake json.RawMessage `json:"user_id"`
	ID          json.RawMessage `json:"id"`
}

type flexibleString struct {
	set bool
	val string
}

func (f *flexibleString) UnmarshalJSON(b []byte) error {
	if f == nil {
		return errors.New("nil flexible string")
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		f.set = false
		f.val = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		f.set = true
		f.val = s
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		f.set = true
		f.val = n.String()
		return nil
	}
	return errors.New("invalid price")
}

type savedSearchDTO struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Q          *string  `json:"q,omitempty"`
	CategoryID *string  `json:"categoryId,omitempty"`
	MinPrice   *string  `json:"minPrice,omitempty"`
	MaxPrice   *string  `json:"maxPrice,omitempty"`
	Currency   *string  `json:"currency,omitempty"`
	North      *float64 `json:"north,omitempty"`
	South      *float64 `json:"south,omitempty"`
	East       *float64 `json:"east,omitempty"`
	West       *float64 `json:"west,omitempty"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
}

type listDTO struct {
	SavedSearches []savedSearchDTO `json:"savedSearches"`
}

type deleteDTO struct {
	ID string `json:"id"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toDTO(row savedsearch.SavedSearch) savedSearchDTO {
	out := savedSearchDTO{
		ID:        row.ID.String(),
		Name:      row.Name,
		Q:         row.Filters.Q,
		MinPrice:  row.Filters.MinPrice,
		MaxPrice:  row.Filters.MaxPrice,
		Currency:  row.Filters.Currency,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.Filters.CategoryID != nil {
		s := row.Filters.CategoryID.String()
		out.CategoryID = &s
	}
	if row.Filters.Viewport != nil {
		n, s, e, w := row.Filters.Viewport.North, row.Filters.Viewport.South, row.Filters.Viewport.East, row.Filters.Viewport.West
		out.North, out.South, out.East, out.West = &n, &s, &e, &w
	}
	return out
}

func writeSavedError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, savedsearch.ErrZeroID), errors.Is(err, savedsearch.ErrInvalid):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, savedsearch.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, savedsearch.ErrUnavailable), errors.Is(err, savedsearch.ErrStoreRequired):
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
	dec.UseNumber()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	return nil
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
