package location

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/db"
)

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) utc() time.Time {
	return f.now
}

func mustService(t *testing.T) (*Service, *MemoryStore, *frozenNow) {
	t.Helper()
	store := NewMemoryStore()
	clock := &frozenNow{now: time.Date(2026, 9, 6, 15, 0, 0, 0, time.UTC)}
	svc, err := NewService(store, clock.utc)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, clock
}

func TestServiceSetReplaceAndGet(t *testing.T) {
	svc, _, clock := mustService(t)
	listingID := mustID(t)
	scope := mustWriteScope(t, listingID)
	first, err := svc.SetListingLocation(context.Background(), scope, Coordinates{Latitude: 36.621, Longitude: 29.116}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ListingID != listingID || first.Latitude != 36.621 || first.Longitude != 29.116 {
		t.Fatalf("first = %+v", first)
	}

	clock.now = clock.now.Add(time.Minute)
	catalog := mustID(t)
	replaced, err := svc.SetListingLocation(context.Background(), scope, Coordinates{Latitude: 41.008, Longitude: 28.978}, &catalog)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Latitude != 41.008 || replaced.Longitude != 28.978 {
		t.Fatalf("replaced coords = %+v", replaced)
	}
	if replaced.CreatedAt.Equal(first.CreatedAt) == false {
		t.Fatalf("created_at must be preserved on replace: first=%v replaced=%v", first.CreatedAt, replaced.CreatedAt)
	}
	if !replaced.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("updated_at must advance: first=%v replaced=%v", first.UpdatedAt, replaced.UpdatedAt)
	}
	if replaced.CatalogLocationID == nil || *replaced.CatalogLocationID != catalog {
		t.Fatalf("catalog = %v", replaced.CatalogLocationID)
	}

	got, err := svc.GetByListingID(context.Background(), listingID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Latitude != 41.008 || got.Longitude != 28.978 {
		t.Fatalf("get = %+v", got)
	}
}

func TestServiceGetNotFound(t *testing.T) {
	svc, _, _ := mustService(t)
	_, err := svc.GetByListingID(context.Background(), mustID(t))
	if !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestServiceSetInvalidCoordinates(t *testing.T) {
	svc, _, _ := mustService(t)
	listingID := mustID(t)
	scope := mustWriteScope(t, listingID)
	if _, err := svc.SetListingLocation(context.Background(), scope, Coordinates{Latitude: 91, Longitude: 0}, nil); !errors.Is(err, errInvalidLatitude) {
		t.Fatalf("lat err = %v", err)
	}
	if _, err := svc.SetListingLocation(context.Background(), scope, Coordinates{Latitude: 0, Longitude: 181}, nil); !errors.Is(err, errInvalidLongitude) {
		t.Fatalf("lon err = %v", err)
	}
}

func TestServiceDeleteDraftCleanup(t *testing.T) {
	svc, _, _ := mustService(t)
	listingID := mustID(t)
	scope := mustWriteScope(t, listingID)
	if _, err := svc.SetListingLocation(context.Background(), scope, Coordinates{Latitude: 36.6, Longitude: 29.1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteByListingID(context.Background(), listingID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetByListingID(context.Background(), listingID); !errors.Is(err, errNotFound) {
		t.Fatalf("after delete err = %v", err)
	}
	if err := svc.DeleteByListingID(context.Background(), listingID); err != nil {
		t.Fatalf("idempotent delete err = %v", err)
	}
}

func TestServiceMapsStoreErrors(t *testing.T) {
	svc, store, _ := mustService(t)
	listingID := mustID(t)
	store.SetFail(db.ErrNoRows)
	if _, err := svc.GetByListingID(context.Background(), listingID); !errors.Is(err, errNotFound) {
		t.Fatalf("no rows = %v", err)
	}
	store.SetFail(db.ErrUnavailable)
	if _, err := svc.SetListingLocation(context.Background(), mustWriteScope(t, listingID), Coordinates{Latitude: 1, Longitude: 2}, nil); !errors.Is(err, errUnavailable) {
		t.Fatalf("unavailable set = %v", err)
	}
	store.SetFail(errors.New("driver boom"))
	if _, err := svc.GetByListingID(context.Background(), listingID); !errors.Is(err, errUnavailable) {
		t.Fatalf("unknown = %v", err)
	}
	if _, err := NewService(nil, nil); err == nil || !errors.Is(err, errStoreRequired) {
		t.Fatalf("nil store err = %v", err)
	}
}

func mustWriteScope(t *testing.T, listingID ID) ListingWriteScope {
	t.Helper()
	scope, err := NewListingWriteScope(listingID)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
