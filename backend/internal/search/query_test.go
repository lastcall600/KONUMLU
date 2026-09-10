package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/db"
)

func TestKeywordSearch(t *testing.T) {
	svc, store := mustQueryService(t)
	bike := mustPublished(t, store, "City Bike", "Used bicycle", nil, nil, nil)
	mustPublished(t, store, "Sofa", "Leather couch", nil, nil, nil)
	page, err := svc.SearchListings(context.Background(), Query{Q: "bike"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Hits) != 1 || page.Hits[0].Doc.ListingID != bike.ListingID {
		t.Fatalf("hits = %+v", page.Hits)
	}
}

func TestCategoryFilter(t *testing.T) {
	svc, store := mustQueryService(t)
	cat := mustID(t)
	want := mustPublished(t, store, "Bike", "Used bicycle", &cat, nil, nil)
	other := mustID(t)
	mustPublished(t, store, "Bike Two", "Used bicycle", &other, nil, nil)
	page, err := svc.SearchListings(context.Background(), Query{CategoryID: &cat})
	if err != nil || len(page.Hits) != 1 || page.Hits[0].Doc.ListingID != want.ListingID {
		t.Fatalf("page = %+v err=%v", page, err)
	}
}

func TestPriceFilters(t *testing.T) {
	svc, store := mustQueryService(t)
	low := "10.00"
	mid := "50.00"
	high := "90.00"
	cur := "TRY"
	mustPublished(t, store, "Cheap", "Used bicycle", nil, &low, &cur)
	want := mustPublished(t, store, "Mid", "Used bicycle", nil, &mid, &cur)
	mustPublished(t, store, "Dear", "Used bicycle", nil, &high, &cur)
	min, max := "40", "60"
	page, err := svc.SearchListings(context.Background(), Query{MinPrice: &min, MaxPrice: &max, Currency: &cur})
	if err != nil || len(page.Hits) != 1 || page.Hits[0].Doc.ListingID != want.ListingID {
		t.Fatalf("page = %+v err=%v", page, err)
	}
}

func TestGeoViewport(t *testing.T) {
	svc, store := mustQueryService(t)
	in := mustPublished(t, store, "In", "Used bicycle", nil, nil, nil)
	setGeo(t, store, in, 36.65, 29.12)
	out := mustPublished(t, store, "Out", "Used bicycle", nil, nil, nil)
	setGeo(t, store, out, 41.0, 29.0)
	page, err := svc.SearchListings(context.Background(), Query{Viewport: &Viewport{
		North: 37, South: 36, East: 30, West: 28,
	}})
	if err != nil || len(page.Hits) != 1 || page.Hits[0].Doc.ListingID != in.ListingID {
		t.Fatalf("page = %+v err=%v", page, err)
	}
}

func TestNoQueryNewestOrdering(t *testing.T) {
	svc, store := mustQueryService(t)
	older := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	first := mustPublishedAt(t, store, "Old", older)
	second := mustPublishedAt(t, store, "New", newer)
	page, err := svc.SearchListings(context.Background(), Query{})
	if err != nil || len(page.Hits) != 2 {
		t.Fatalf("page = %+v err=%v", page, err)
	}
	if page.Hits[0].Doc.ListingID != second.ListingID || page.Hits[1].Doc.ListingID != first.ListingID {
		t.Fatalf("order = %s then %s", page.Hits[0].Doc.Title, page.Hits[1].Doc.Title)
	}
}

func TestCursorNextPage(t *testing.T) {
	svc, store := mustQueryService(t)
	t1 := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	oldest := mustPublishedAt(t, store, "A", t1)
	mustPublishedAt(t, store, "B", t2)
	newest := mustPublishedAt(t, store, "C", t3)
	page1, err := svc.SearchListings(context.Background(), Query{Limit: 2})
	if err != nil || len(page1.Hits) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v err=%v", page1, err)
	}
	if page1.Hits[0].Doc.ListingID != newest.ListingID {
		t.Fatalf("first = %s", page1.Hits[0].Doc.Title)
	}
	page2, err := svc.SearchListings(context.Background(), Query{Limit: 2, Cursor: page1.NextCursor})
	if err != nil || len(page2.Hits) != 1 || page2.Hits[0].Doc.ListingID != oldest.ListingID {
		t.Fatalf("page2 = %+v err=%v", page2, err)
	}
	if page2.NextCursor != "" {
		t.Fatalf("expected no further page: %s", page2.NextCursor)
	}
}

func TestUnpublishedRowsNeverReturned(t *testing.T) {
	svc, store := mustQueryService(t)
	pub := mustPublished(t, store, "Bike", "Used bicycle", nil, nil, nil)
	draft := pub
	draft.ListingID = mustID(t)
	draft.Status = "draft"
	draft.Title = "Hidden draft"
	if err := store.Upsert(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	page, err := svc.SearchListings(context.Background(), Query{Q: "bike"})
	if err != nil || len(page.Hits) != 1 || page.Hits[0].Doc.ListingID != pub.ListingID {
		t.Fatalf("page = %+v err=%v", page, err)
	}
}

func TestMalformedQuery(t *testing.T) {
	svc, _ := mustQueryService(t)
	if _, err := svc.SearchListings(context.Background(), Query{Limit: 99}); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("limit err = %v", err)
	}
	badPrice := "x"
	if _, err := svc.SearchListings(context.Background(), Query{MinPrice: &badPrice}); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("price err = %v", err)
	}
	if _, err := svc.SearchListings(context.Background(), Query{Cursor: "%%%"}); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("cursor err = %v", err)
	}
	if _, err := svc.SearchListings(context.Background(), Query{Viewport: &Viewport{North: 1, South: 2, East: 3, West: 1}}); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("geo err = %v", err)
	}
}

func TestSearchUnavailable(t *testing.T) {
	store := NewMemoryStore()
	store.SetFail(db.ErrUnavailable)
	svc, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SearchListings(context.Background(), Query{}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestPostgresSearchRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	if _, err := p.Search(context.Background(), NormalizedQuery{Limit: 1}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func mustQueryService(t *testing.T) (*Service, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	svc, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store
}

func mustPublished(t *testing.T, store *MemoryStore, title string, desc string, category *ID, amount, currency *string) ListingDocument {
	t.Helper()
	pub := time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)
	return mustPublishedAtDoc(t, store, title, desc, category, amount, currency, pub)
}

func mustPublishedAt(t *testing.T, store *MemoryStore, title string, at time.Time) ListingDocument {
	t.Helper()
	return mustPublishedAtDoc(t, store, title, "Used bicycle", nil, nil, nil, at)
}

func mustPublishedAtDoc(t *testing.T, store *MemoryStore, title, desc string, category *ID, amount, currency *string, at time.Time) ListingDocument {
	t.Helper()
	id := mustID(t)
	cat := id
	if category != nil {
		cat = *category
	}
	pub := at.UTC()
	doc := ListingDocument{
		ListingID:             id,
		Status:                StatusPublished,
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
	if err := store.Upsert(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func setGeo(t *testing.T, store *MemoryStore, doc ListingDocument, lat, lon float64) {
	t.Helper()
	got, err := store.Get(context.Background(), doc.ListingID)
	if err != nil {
		t.Fatal(err)
	}
	got.Latitude = &lat
	got.Longitude = &lon
	if err := store.Upsert(context.Background(), got); err != nil {
		t.Fatal(err)
	}
}
