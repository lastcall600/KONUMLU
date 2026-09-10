package publicprofile

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/identity"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (identity.ID, error)
}

// Handler is the public-profile HTTP adapter. It does not own Identity rules.
type Handler struct {
	sessions sessionResolver
	svc      *Service
	origins  map[string]struct{}
}

func NewHandler(sessions sessionResolver, svc *Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/profile/me", h.getMe)
	mux.HandleFunc("PATCH /v1/profile/me", h.patchMe)
	mux.HandleFunc("GET /v1/public/profiles/{publicProfileId}", h.getPublic)
}

func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	view, err := h.svc.GetMe(r.Context(), userID)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(view))
}

func (h *Handler) patchMe(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	var req patchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.UserID) != "" || strings.TrimSpace(req.UserIDSnake) != "" ||
		req.ModerationState != nil || req.ModerationStateSnake != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	view, err := h.svc.UpdateMe(r.Context(), userID, req.DisplayName)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(view))
}

func (h *Handler) getPublic(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.PathValue("publicProfileId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	id, err := ParsePublicID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	view, err := h.svc.GetPublic(r.Context(), id)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(view))
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (identity.ID, bool) {
	if !h.requireOrigin(w, r) {
		return identity.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return identity.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return identity.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (identity.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return identity.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return identity.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return identity.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return identity.ID{}, false
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

func hasForeignUserID(r *http.Request) bool {
	q := r.URL.Query()
	return strings.TrimSpace(q.Get("userId")) != "" || strings.TrimSpace(q.Get("user_id")) != ""
}

type patchRequest struct {
	DisplayName          *string `json:"displayName"`
	UserID               string  `json:"userId"`
	UserIDSnake          string  `json:"user_id"`
	ModerationState      *string `json:"moderationState"`
	ModerationStateSnake *string `json:"moderation_state"`
}

type profileDTO struct {
	PublicProfileID string  `json:"publicProfileId"`
	DisplayName     *string `json:"displayName"`
	MemberSince     string  `json:"memberSince"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toDTO(v PublicView) profileDTO {
	return profileDTO{
		PublicProfileID: v.PublicProfileID.String(),
		DisplayName:     cloneDisplay(v.DisplayName),
		MemberSince:     v.MemberSince.UTC().Format(time.RFC3339),
	}
}

func writeProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidPublicID), errors.Is(err, ErrInvalidDisplayName), errors.Is(err, errZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrStoreRequired):
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
