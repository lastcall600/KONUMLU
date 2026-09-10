package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/platform/db"
	"backend/internal/search"
)

func TestKeywordSearchHTTP(t *testing.T) {
	h := newSearchHandler(t)
	bike := seedPublished(t, h, "City Bike", "Used bicycle", nil, nil, nil, nil)
	seedPublished(t, h, "Sofa", "Leather couch", nil, nil, nil, nil)
	rec := get(t, h, "/v1/search/listings?q=bike")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body searchResponse
	decode(t, rec, &body)
	if len(body.Listings) != 1 || body.Listings[0].ListingID != bike.ListingID.String() {
		t.Fatalf("body = %+v", body)
	}
	if body.Listings[0].Title != "City Bike" {
		t.Fatalf("title = %s", body.Listings[0].Title)
	}
}

func TestCategoryFilterHTTP(t *testing.T) {
	h := newSearchHandler(t)
	cat, err := search.NewID()
	if err != nil {
		t.Fatal(err)
	}
	want := seedPublished(t, h, "Bike", "Used bicycle", &cat, nil, nil, nil)
	other, err := search.NewID()
	if err != nil {
		t.Fatal(err)
	}
	seedPublished(t, h, "Bike Two", "Used bicycle", &other, nil, nil, nil)
	rec := get(t, h, "/v1/search/listings?categoryId="+cat.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body searchResponse
	decode(t, rec, &body)
	if len(body.Listings) != 1 || body.Listings[0].ListingID != want.ListingID.String() {
		t.Fatalf("body = %+v", body)
	}
}

func TestPriceFiltersHTTP(t *testing.T) {
	h := newSearchHandler(t)
	low, mid, high, cur := "10.00", "50.00", "90.00", "TRY"
	seedPublished(t, h, "Cheap", "Used bicycle", nil, &low, &cur, nil)
	want := seedPublished(t, h, "Mid", "Used bicycle", nil, &mid, &cur, nil)
	seedPublished(t, h, "Dear", "Used bicycle", nil, &high, &cur, nil)
	rec := get(t, h, "/v1/search/listings?minPrice=40&maxPrice=60&currency=TRY")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body searchResponse
	decode(t, rec, &body)
	if len(body.Listings) != 1 || body.Listings[0].ListingID != want.ListingID.String() {
		t.Fatalf("body = %+v", body)
	}
}

func TestGeoViewportHTTP(t *testing.T) {
	h := newSearchHandler(t)
	in := seedPublished(t, h, "In", "Used bicycle", nil, nil, nil, &geo{36.65, 29.12})
	seedPublished(t, h, "Out", "Used bicycle", nil, nil, nil, &geo{41, 29})
	rec := get(t, h, "/v1/search/listings?north=37&south=36&east=30&west=28")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body searchResponse
	decode(t, rec, &body)
	if len(body.Listings) != 1 || body.Listings[0].ListingID != in.ListingID.String() {
		t.Fatalf("body = %+v", body)
	}
	if body.Listings[0].Latitude == nil || *body.Listings[0].Latitude != 36.65 {
		t.Fatalf("geo = %+v", body.Listings[0])
	}
}

func TestNoQueryNewestHTTP(t *testing.T) {
	h := newSearchHandler(t)
	older := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	seedPublishedAt(t, h, "Old", older, nil)
	second := seedPublishedAt(t, h, "New", newer, nil)
	rec := get(t, h, "/v1/search/listings")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body searchResponse
	decode(t, rec, &body)
	if len(body.Listings) != 2 || body.Listings[0].ListingID != second.ListingID.String() {
		t.Fatalf("body = %+v", body)
	}
}

func TestCursorNextPageHTTP(t *testing.T) {
	h := newSearchHandler(t)
	t1 := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	oldest := seedPublishedAt(t, h, "A", t1, nil)
	seedPublishedAt(t, h, "B", t2, nil)
	seedPublishedAt(t, h, "C", t3, nil)
	rec := get(t, h, "/v1/search/listings?limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var page1 searchResponse
	decode(t, rec, &page1)
	if page1.NextCursor == nil || len(page1.Listings) != 2 {
		t.Fatalf("page1 = %+v", page1)
	}
	rec = get(t, h, "/v1/search/listings?limit=2&cursor="+*page1.NextCursor)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var page2 searchResponse
	decode(t, rec, &page2)
	if len(page2.Listings) != 1 || page2.Listings[0].ListingID != oldest.ListingID.String() {
		t.Fatalf("page2 = %+v", page2)
	}
}

func TestMalformedQueryHTTP400(t *testing.T) {
	h := newSearchHandler(t)
	cases := []string{
		"/v1/search/listings?categoryId=not-a-uuid",
		"/v1/search/listings?minPrice=abc",
		"/v1/search/listings?north=1",
		"/v1/search/listings?limit=foo",
		"/v1/search/listings?cursor=not-a-cursor",
		"/v1/search/listings?limit=99",
	}
	for _, path := range cases {
		rec := get(t, h, path)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec, "bad_request")
	}
}

func TestSearchDBUnavailableHTTP503(t *testing.T) {
	store := search.NewMemoryStore()
	store.SetFail(db.ErrUnavailable)
	svc, err := search.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(svc)
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/v1/search/listings")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	if strings.Contains(strings.ToLower(rec.Body.String()), "postgres") {
		t.Fatal("must not leak db details")
	}
}

func TestUnpublishedNeverReturnedHTTP(t *testing.T) {
	h := newSearchHandler(t)
	pub := seedPublished(t, h, "Bike", "Used bicycle", nil, nil, nil, nil)
	draft := pub
	id, err := search.NewID()
	if err != nil {
		t.Fatal(err)
	}
	draft.ListingID = id
	draft.Status = "draft"
	draft.Title = "Hidden"
	if err := h.store.Upsert(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/v1/search/listings?q=bike")
	var body searchResponse
	decode(t, rec, &body)
	if rec.Code != http.StatusOK || len(body.Listings) != 1 || body.Listings[0].ListingID != pub.ListingID.String() {
		t.Fatalf("body = %+v status=%d", body, rec.Code)
	}
}

func TestEmptySearchIs200Array(t *testing.T) {
	h := newSearchHandler(t)
	rec := get(t, h, "/v1/search/listings?q=nothing")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body searchResponse
	decode(t, rec, &body)
	if body.Listings == nil || len(body.Listings) != 0 {
		t.Fatalf("listings = %#v", body.Listings)
	}
}

type testSearchHandler struct {
	*Handler
	store *search.MemoryStore
}

type geo struct {
	lat, lon float64
}

func newSearchHandler(t *testing.T) *testSearchHandler {
	t.Helper()
	store := search.NewMemoryStore()
	svc, err := search.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(svc)
	if err != nil {
		t.Fatal(err)
	}
	return &testSearchHandler{Handler: h, store: store}
}

func seedPublished(t *testing.T, h *testSearchHandler, title, desc string, category *search.ID, amount, currency *string, g *geo) search.ListingDocument {
	t.Helper()
	return seedPublishedAt(t, h, title, time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC), g, desc, category, amount, currency)
}

func seedPublishedAt(t *testing.T, h *testSearchHandler, title string, at time.Time, g *geo, extra ...any) search.ListingDocument {
	t.Helper()
	desc := "Used bicycle"
	var category *search.ID
	var amount, currency *string
	if len(extra) > 0 {
		if s, ok := extra[0].(string); ok {
			desc = s
		}
	}
	if len(extra) > 1 {
		if c, ok := extra[1].(*search.ID); ok {
			category = c
		}
	}
	if len(extra) > 2 {
		if a, ok := extra[2].(*string); ok {
			amount = a
		}
	}
	if len(extra) > 3 {
		if c, ok := extra[3].(*string); ok {
			currency = c
		}
	}
	id, err := search.NewID()
	if err != nil {
		t.Fatal(err)
	}
	cat := id
	if category != nil {
		cat = *category
	}
	pub := at.UTC()
	doc := search.ListingDocument{
		ListingID:             id,
		Status:                search.StatusPublished,
		CategoryID:            cat,
		CategorySchemaVersion: 1,
		Title:                 title,
		Description:           desc,
		PriceAmount:           amount,
		PriceCurrency:         currency,
		Attributes:            map[string]any{},
		PublishedAt:           &pub,
		UpdatedAt:             pub,
	}
	if g != nil {
		lat, lon := g.lat, g.lon
		doc.Latitude = &lat
		doc.Longitude = &lon
	}
	if err := h.store.Upsert(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func get(t *testing.T, h interface{ Register(*http.ServeMux) }, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(rec, r)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dest); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body errorResponse
	decode(t, rec, &body)
	if body.Error != code {
		t.Fatalf("error = %q want %q body=%s", body.Error, code, rec.Body.String())
	}
}
