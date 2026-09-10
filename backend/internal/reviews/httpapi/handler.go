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

	"backend/internal/reviews"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
	maxJSONBytes      = 16 << 10
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (reviews.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *reviews.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *reviews.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, reviews.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, reviews.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, reviews.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/reviews", h.create)
	mux.HandleFunc("GET /v1/reviews/mine", h.mine)
	mux.HandleFunc("GET /v1/reviews/eligibility/{verifiedInteractionId}", h.eligibility)
	mux.HandleFunc("GET /v1/public/listings/{listingId}/reviews", h.publicListing)
}

func (h *Handler) publicListing(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return
	}
	q := reviews.PublicListQuery{ListingID: listingID, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor"))}
	if r.URL.Query().Has("limit") {
		n, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		q.Limit = n
	}
	page, err := h.svc.ListPublicForListing(r.Context(), q)
	if err != nil {
		writeReviewsError(w, err)
		return
	}
	out := make([]publicReviewDTO, 0, len(page.Reviews))
	for _, row := range page.Reviews {
		out = append(out, toPublicReviewDTO(row))
	}
	writeJSON(w, http.StatusOK, publicReviewListDTO{Reviews: out, NextCursor: omitEmpty(page.NextCursor)})
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
		writeReviewsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toReviewDTO(row))
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.ListMine(r.Context(), userID)
	if err != nil {
		writeReviewsError(w, err)
		return
	}
	out := make([]reviewDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toReviewDTO(row))
	}
	writeJSON(w, http.StatusOK, reviewListDTO{Reviews: out})
}

func (h *Handler) eligibility(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	interactionID, ok := parseInteractionID(w, r)
	if !ok {
		return
	}
	got, err := h.svc.Eligibility(r.Context(), userID, interactionID)
	if err != nil {
		writeReviewsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toEligibilityDTO(got))
}

func parseCreate(w http.ResponseWriter, r *http.Request) (reviews.CreateInput, bool) {
	var req createRequest
	if !decodeJSON(w, r, &req) {
		return reviews.CreateInput{}, false
	}
	if len(req.UserID) > 0 || len(req.ReviewerUserID) > 0 || len(req.ProviderUserID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return reviews.CreateInput{}, false
	}
	id, err := reviews.ParseID(strings.TrimSpace(req.VerifiedInteractionID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return reviews.CreateInput{}, false
	}
	if req.ListingAccuracy == nil || req.ProviderService == nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return reviews.CreateInput{}, false
	}
	return reviews.CreateInput{
		VerifiedInteractionID: id,
		Body:                  req.Body,
		ListingAccuracy:       *req.ListingAccuracy,
		ProviderService:       *req.ProviderService,
	}, true
}

func parseListingID(w http.ResponseWriter, r *http.Request) (reviews.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("listingId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return reviews.ID{}, false
	}
	id, err := reviews.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return reviews.ID{}, false
	}
	return id, true
}

func parseInteractionID(w http.ResponseWriter, r *http.Request) (reviews.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("verifiedInteractionId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return reviews.ID{}, false
	}
	id, err := reviews.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return reviews.ID{}, false
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

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (reviews.ID, bool) {
	if !h.requireOrigin(w, r) {
		return reviews.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return reviews.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return reviews.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (reviews.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return reviews.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return reviews.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return reviews.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return reviews.ID{}, false
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
	VerifiedInteractionID string          `json:"verifiedInteractionId"`
	Body                  string          `json:"body"`
	ListingAccuracy       *int            `json:"listingAccuracy"`
	ProviderService       *int            `json:"providerService"`
	UserID                json.RawMessage `json:"userId"`
	ReviewerUserID        json.RawMessage `json:"reviewerUserId"`
	ProviderUserID        json.RawMessage `json:"providerUserId"`
}

type reviewDTO struct {
	ReviewID              string  `json:"reviewId"`
	VerifiedInteractionID string  `json:"verifiedInteractionId"`
	ListingID             string  `json:"listingId"`
	ListingAccuracy       int     `json:"listingAccuracy"`
	ProviderService       int     `json:"providerService"`
	Body                  *string `json:"body"`
	CreatedAt             string  `json:"createdAt"`
	UpdatedAt             string  `json:"updatedAt"`
}

type reviewListDTO struct {
	Reviews []reviewDTO `json:"reviews"`
}

type publicReviewDTO struct {
	ReviewID        string  `json:"reviewId"`
	Body            *string `json:"body"`
	ListingAccuracy int     `json:"listingAccuracy"`
	ProviderService int     `json:"providerService"`
	CreatedAt       string  `json:"createdAt"`
}

type publicReviewListDTO struct {
	Reviews    []publicReviewDTO `json:"reviews"`
	NextCursor *string           `json:"nextCursor,omitempty"`
}

type eligibilityDTO struct {
	Eligible        bool    `json:"eligible"`
	ExpiresAt       *string `json:"expiresAt,omitempty"`
	AlreadyReviewed bool    `json:"alreadyReviewed"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toReviewDTO(row reviews.Review) reviewDTO {
	return reviewDTO{
		ReviewID:              row.ID.String(),
		VerifiedInteractionID: row.VerifiedInteractionID.String(),
		ListingID:             row.ListingID.String(),
		ListingAccuracy:       row.ListingAccuracy,
		ProviderService:       row.ProviderService,
		Body:                  row.Body,
		CreatedAt:             row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:             row.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toPublicReviewDTO(row reviews.PublicReview) publicReviewDTO {
	return publicReviewDTO{
		ReviewID:        row.ID.String(),
		Body:            row.Body,
		ListingAccuracy: row.ListingAccuracy,
		ProviderService: row.ProviderService,
		CreatedAt:       row.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func toEligibilityDTO(row reviews.Eligibility) eligibilityDTO {
	dto := eligibilityDTO{
		Eligible:        row.Eligible,
		AlreadyReviewed: row.AlreadyReviewed,
	}
	if row.ExpiresAt != nil {
		at := row.ExpiresAt.UTC().Format(time.RFC3339)
		dto.ExpiresAt = &at
	}
	return dto
}

func writeReviewsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, reviews.ErrZeroID), errors.Is(err, reviews.ErrInvalidBody),
		errors.Is(err, reviews.ErrInvalidRating), errors.Is(err, reviews.ErrNotEligible),
		errors.Is(err, reviews.ErrInvalidQuery):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, reviews.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, reviews.ErrConflict):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, reviews.ErrUnavailable), errors.Is(err, reviews.ErrStoreRequired),
		errors.Is(err, reviews.ErrVerifiedReq), errors.Is(err, reviews.ErrOutboxRequired),
		errors.Is(err, reviews.ErrListingsReq):
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func omitEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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
