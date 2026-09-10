package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/search"
)

type Handler struct {
	svc *search.Service
}

func New(svc *search.Service) (*Handler, error) {
	if svc == nil {
		return nil, search.ErrStoreRequired
	}
	return &Handler{svc: svc}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/search/listings", h.searchListings)
}

func (h *Handler) searchListings(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	q, ok := parseSearchQuery(w, r)
	if !ok {
		return
	}
	page, err := h.svc.SearchListings(r.Context(), q)
	if err != nil {
		writeSearchError(w, err)
		return
	}
	out := make([]listingResultDTO, 0, len(page.Hits))
	for _, hit := range page.Hits {
		out = append(out, toResultDTO(hit.Doc))
	}
	writeJSON(w, http.StatusOK, searchResponse{Listings: out, NextCursor: omitEmpty(page.NextCursor)})
}

func parseSearchQuery(w http.ResponseWriter, r *http.Request) (search.Query, bool) {
	values := r.URL.Query()
	q := search.Query{Q: strings.TrimSpace(values.Get("q"))}
	if raw := strings.TrimSpace(values.Get("categoryId")); raw != "" {
		id, err := search.ParseID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return search.Query{}, false
		}
		q.CategoryID = &id
	}
	if values.Has("minPrice") {
		v := strings.TrimSpace(values.Get("minPrice"))
		q.MinPrice = &v
	}
	if values.Has("maxPrice") {
		v := strings.TrimSpace(values.Get("maxPrice"))
		q.MaxPrice = &v
	}
	if values.Has("currency") {
		v := strings.TrimSpace(values.Get("currency"))
		q.Currency = &v
	}
	north := strings.TrimSpace(values.Get("north"))
	south := strings.TrimSpace(values.Get("south"))
	east := strings.TrimSpace(values.Get("east"))
	west := strings.TrimSpace(values.Get("west"))
	geoCount := countNonEmpty(north, south, east, west)
	if geoCount != 0 && geoCount != 4 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return search.Query{}, false
	}
	if geoCount == 4 {
		n, err1 := strconv.ParseFloat(north, 64)
		s, err2 := strconv.ParseFloat(south, 64)
		e, err3 := strconv.ParseFloat(east, 64)
		we, err4 := strconv.ParseFloat(west, 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return search.Query{}, false
		}
		q.Viewport = &search.Viewport{North: n, South: s, East: e, West: we}
	}
	q.Cursor = strings.TrimSpace(values.Get("cursor"))
	if values.Has("limit") {
		n, err := strconv.Atoi(strings.TrimSpace(values.Get("limit")))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return search.Query{}, false
		}
		q.Limit = n
	}
	return q, true
}

func countNonEmpty(vals ...string) int {
	n := 0
	for _, v := range vals {
		if v != "" {
			n++
		}
	}
	return n
}

type listingResultDTO struct {
	ListingID     string   `json:"listingId"`
	CategoryID    string   `json:"categoryId"`
	Title         string   `json:"title"`
	PriceAmount   *string  `json:"priceAmount,omitempty"`
	PriceCurrency *string  `json:"priceCurrency,omitempty"`
	Latitude      *float64 `json:"latitude,omitempty"`
	Longitude     *float64 `json:"longitude,omitempty"`
	PublishedAt   *string  `json:"publishedAt,omitempty"`
}

type searchResponse struct {
	Listings   []listingResultDTO `json:"listings"`
	NextCursor *string            `json:"nextCursor,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toResultDTO(d search.ListingDocument) listingResultDTO {
	return listingResultDTO{
		ListingID:     d.ListingID.String(),
		CategoryID:    d.CategoryID.String(),
		Title:         d.Title,
		PriceAmount:   d.PriceAmount,
		PriceCurrency: d.PriceCurrency,
		Latitude:      d.Latitude,
		Longitude:     d.Longitude,
		PublishedAt:   formatTimePtr(d.PublishedAt),
	}
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func omitEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func writeSearchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, search.ErrInvalidQuery), errors.Is(err, search.ErrInvalidEvent),
		errors.Is(err, search.ErrZeroID), errors.Is(err, search.ErrInvalidDoc):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, search.ErrUnavailable), errors.Is(err, search.ErrStoreRequired):
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
