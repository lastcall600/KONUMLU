package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/trust"
)

const sessionCookieName = "__Host-konumlu_session"

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (trust.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *trust.Service
	profiles identitycontracts.PublicProfileResolver
}

func New(sessions sessionResolver, svc *trust.Service, profiles identitycontracts.PublicProfileResolver, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil || profiles == nil {
		return nil, trust.ErrUnavailable
	}
	origins := 0
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, trust.ErrUnavailable
		}
		origins++
	}
	if origins == 0 {
		return nil, trust.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, profiles: profiles}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/trust/me", h.me)
	mux.HandleFunc("GET /v1/public/profiles/{publicProfileId}/trust", h.publicByProfile)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	profile, err := h.svc.GetMe(r.Context(), userID)
	if err != nil {
		writeTrustError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMeDTO(profile))
}

func (h *Handler) publicByProfile(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	raw := strings.TrimSpace(r.PathValue("publicProfileId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	publicID, err := parsePublicProfileID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if _, err := h.profiles.ResolveByPublicID(r.Context(), publicID); err != nil {
		writePublicProfileError(w, err)
		return
	}
	userID, err := h.profiles.ResolveUserIDByPublicID(r.Context(), publicID)
	if err != nil {
		writePublicProfileError(w, err)
		return
	}
	passport, err := h.svc.GetPublic(r.Context(), trust.ID(userID))
	if err != nil {
		writeTrustError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPublicDTO(passport))
}

func parsePublicProfileID(raw string) (identitycontracts.ID, error) {
	id, err := trust.ParseID(raw)
	if err != nil {
		return identitycontracts.ID{}, err
	}
	return identitycontracts.ID(id), nil
}

func hasForeignUserID(r *http.Request) bool {
	q := r.URL.Query()
	return strings.TrimSpace(q.Get("userId")) != "" || strings.TrimSpace(q.Get("user_id")) != ""
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (trust.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return trust.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return trust.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return trust.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return trust.ID{}, false
	}
	return userID, true
}

type meDTO struct {
	// Level is derived only from listing_inspection verifiedInteractionCount
	// (0 new, 1–4 verified, 5+ established). Transaction and delivery completions
	// do not change this count or level. Review counters and
	// providerServiceAverage are displayed separately and are not weighted into a hidden score.
	Level                             string       `json:"level"`
	VerifiedInteractionCount          int          `json:"verifiedInteractionCount"`
	RequesterVerifiedInteractionCount int          `json:"requesterVerifiedInteractionCount"`
	ProviderVerifiedInteractionCount  int          `json:"providerVerifiedInteractionCount"`
	LastVerifiedInteractionAt         *string      `json:"lastVerifiedInteractionAt"`
	VerifiedReviewCount               int          `json:"verifiedReviewCount"`
	ProviderServiceReviewCount        int          `json:"providerServiceReviewCount"`
	ProviderServiceAverage            *json.Number `json:"providerServiceAverage"`
	LastVerifiedReviewAt              *string      `json:"lastVerifiedReviewAt"`
}

func toMeDTO(p trust.UserProfile) meDTO {
	return meDTO{
		Level:                             p.TrustLevel,
		VerifiedInteractionCount:          p.VerifiedInteractionCount,
		RequesterVerifiedInteractionCount: p.RequesterVerifiedInteractionCount,
		ProviderVerifiedInteractionCount:  p.ProviderVerifiedInteractionCount,
		LastVerifiedInteractionAt:         formatTimePtr(p.LastVerifiedInteractionAt),
		VerifiedReviewCount:               p.VerifiedReviewCount,
		ProviderServiceReviewCount:        p.ProviderServiceReviewCount,
		ProviderServiceAverage:            p.ProviderServiceAverage(),
		LastVerifiedReviewAt:              formatTimePtr(p.LastVerifiedReviewAt),
	}
}

type publicDTO struct {
	Level                            string       `json:"level"`
	VerifiedInteractionCount         int          `json:"verifiedInteractionCount"`
	ProviderVerifiedInteractionCount int          `json:"providerVerifiedInteractionCount"`
	ProviderServiceReviewCount       int          `json:"providerServiceReviewCount"`
	ProviderServiceAverage           *json.Number `json:"providerServiceAverage"`
	LastVerifiedInteractionAt        *string      `json:"lastVerifiedInteractionAt"`
}

func toPublicDTO(p trust.PublicPassport) publicDTO {
	return publicDTO{
		Level:                            p.Level,
		VerifiedInteractionCount:         p.VerifiedInteractionCount,
		ProviderVerifiedInteractionCount: p.ProviderVerifiedInteractionCount,
		ProviderServiceReviewCount:       p.ProviderServiceReviewCount,
		ProviderServiceAverage:           p.ProviderServiceAverage,
		LastVerifiedInteractionAt:        formatTimePtr(p.LastVerifiedInteractionAt),
	}
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

type errorResponse struct {
	Error string `json:"error"`
}

func writePublicProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identitycontracts.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, identitycontracts.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func writeTrustError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, trust.ErrZeroID), errors.Is(err, trust.ErrInvalidEvent),
		errors.Is(err, trust.ErrInvalidProfile), errors.Is(err, trust.ErrInvalidPolicy):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, trust.ErrUnavailable), errors.Is(err, trust.ErrStoreRequired):
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
