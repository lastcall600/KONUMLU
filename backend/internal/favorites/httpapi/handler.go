package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/favorites"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (favorites.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *favorites.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *favorites.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, favorites.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, favorites.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, favorites.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/favorites/listings/{listingId}", h.add)
	mux.HandleFunc("DELETE /v1/favorites/listings/{listingId}", h.remove)
	mux.HandleFunc("GET /v1/favorites/listings", h.list)
	mux.HandleFunc("GET /v1/favorites/listings/{listingId}", h.get)
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Add(r.Context(), userID, listingID); err != nil {
		writeFavoriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, favoriteStateDTO{ListingID: listingID.String(), Favorited: true})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Remove(r.Context(), userID, listingID); err != nil {
		writeFavoriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, favoriteStateDTO{ListingID: listingID.String(), Favorited: false})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return
	}
	okFav, err := h.svc.IsFavorited(r.Context(), userID, listingID)
	if err != nil {
		writeFavoriteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, favoriteStateDTO{ListingID: listingID.String(), Favorited: okFav})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.ListVisible(r.Context(), userID)
	if err != nil {
		writeFavoriteError(w, err)
		return
	}
	out := make([]favoriteRecordDTO, 0, len(rows))
	for _, fav := range rows {
		out = append(out, favoriteRecordDTO{
			ListingID: fav.ListingID.String(),
			CreatedAt: fav.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, favoriteListDTO{Listings: out})
}

func parseListingID(w http.ResponseWriter, r *http.Request) (favorites.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("listingId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return favorites.ID{}, false
	}
	id, err := favorites.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return favorites.ID{}, false
	}
	return id, true
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (favorites.ID, bool) {
	if !h.requireOrigin(w, r) {
		return favorites.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return favorites.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return favorites.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (favorites.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return favorites.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return favorites.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return favorites.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return favorites.ID{}, false
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

type favoriteStateDTO struct {
	ListingID string `json:"listingId"`
	Favorited bool   `json:"favorited"`
}

type favoriteRecordDTO struct {
	ListingID string `json:"listingId"`
	CreatedAt string `json:"createdAt"`
}

type favoriteListDTO struct {
	Listings []favoriteRecordDTO `json:"listings"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeFavoriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, favorites.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, favorites.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, favorites.ErrUnavailable), errors.Is(err, favorites.ErrStoreRequired),
		errors.Is(err, favorites.ErrListingsReq):
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
