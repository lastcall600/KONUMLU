package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"backend/internal/reviewaggregates"
)

const sessionCookieName = "__Host-konumlu_session"

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (reviewaggregates.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *reviewaggregates.Service
}

func New(sessions sessionResolver, svc *reviewaggregates.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, reviewaggregates.ErrUnavailable
	}
	origins := 0
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, reviewaggregates.ErrUnavailable
		}
		origins++
	}
	if origins == 0 {
		return nil, reviewaggregates.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/public/listings/{listingId}/review-summary", h.publicListing)
	mux.HandleFunc("GET /v1/review-summary/me", h.me)
}

func (h *Handler) publicListing(w http.ResponseWriter, r *http.Request) {
	listingID, err := reviewaggregates.ParseID(r.PathValue("listingId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	got, err := h.svc.ListingAccuracy(r.Context(), listingID)
	if err != nil {
		writeSummaryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listingSummaryDTO{
		ListingAccuracy: toRatingDTO(got),
	})
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
	got, err := h.svc.ProviderServiceMe(r.Context(), userID)
	if err != nil {
		writeSummaryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meSummaryDTO{
		ProviderService: toRatingDTO(got),
	})
}

func hasForeignUserID(r *http.Request) bool {
	q := r.URL.Query()
	return strings.TrimSpace(q.Get("userId")) != "" || strings.TrimSpace(q.Get("user_id")) != ""
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (reviewaggregates.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return reviewaggregates.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return reviewaggregates.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return reviewaggregates.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return reviewaggregates.ID{}, false
	}
	return userID, true
}

type listingSummaryDTO struct {
	ListingAccuracy ratingDTO `json:"listingAccuracy"`
}

type meSummaryDTO struct {
	ProviderService ratingDTO `json:"providerService"`
}

type ratingDTO struct {
	ReviewCount int          `json:"reviewCount"`
	Average     *json.Number `json:"average"`
}

func toRatingDTO(s reviewaggregates.RatingSummary) ratingDTO {
	return ratingDTO{
		ReviewCount: s.ReviewCount,
		Average:     s.AverageNumber(),
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeSummaryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, reviewaggregates.ErrZeroID), errors.Is(err, reviewaggregates.ErrInvalidEvent),
		errors.Is(err, reviewaggregates.ErrInvalidRating), errors.Is(err, reviewaggregates.ErrInvalidSummary):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, reviewaggregates.ErrUnavailable), errors.Is(err, reviewaggregates.ErrStoreRequired):
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
