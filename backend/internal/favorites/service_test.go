package favorites

import (
	"context"
	"errors"
	"testing"
	"time"

	listingcontracts "backend/internal/listings/contracts"
)

func TestAddIsIdempotentAndSessionScoped(t *testing.T) {
	svc, store, listings := newTestService(t)
	user, listing := mustID(t), mustID(t)
	listings.setPublished(listing)
	if err := svc.Add(context.Background(), user, listing); err != nil {
		t.Fatal(err)
	}
	if err := svc.Add(context.Background(), user, listing); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListByUser(context.Background(), user)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %v err=%v", rows, err)
	}
	other := mustID(t)
	got, err := store.Get(context.Background(), other, listing)
	if !errors.Is(err, errNotFound) || got.UserID != (ID{}) {
		t.Fatalf("cross-user get = %+v err=%v", got, err)
	}
}

func TestAddRequiresPublicListing(t *testing.T) {
	svc, _, listings := newTestService(t)
	user, listing := mustID(t), mustID(t)
	if err := svc.Add(context.Background(), user, listing); !errors.Is(err, errNotFound) {
		t.Fatalf("missing listing err = %v", err)
	}
	listings.snaps[listingcontracts.ID(listing)] = listingcontracts.ListingSnapshot{
		ID: listingcontracts.ID(listing), Status: "draft",
	}
	if err := svc.Add(context.Background(), user, listing); !errors.Is(err, errNotFound) {
		t.Fatalf("draft listing err = %v", err)
	}
}

func TestRemoveMissingIsIdempotent(t *testing.T) {
	svc, _, _ := newTestService(t)
	if err := svc.Remove(context.Background(), mustID(t), mustID(t)); err != nil {
		t.Fatal(err)
	}
}

func TestListVisibleOmitsNonPublic(t *testing.T) {
	svc, _, listings := newTestService(t)
	user := mustID(t)
	pub, hidden := mustID(t), mustID(t)
	listings.setPublished(pub)
	listings.snaps[listingcontracts.ID(hidden)] = listingcontracts.ListingSnapshot{
		ID: listingcontracts.ID(hidden), Status: "archived",
	}
	if err := svc.Add(context.Background(), user, pub); err != nil {
		t.Fatal(err)
	}
	store := svc.store
	if err := store.Add(context.Background(), Favorite{
		UserID: user, ListingID: hidden, CreatedAt: time.Unix(2, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	visible, err := svc.ListVisible(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].ListingID != pub {
		t.Fatalf("visible = %+v", visible)
	}
	ok, err := svc.IsFavorited(context.Background(), user, hidden)
	if err != nil || !ok {
		t.Fatalf("hidden still stored internally ok=%v err=%v", ok, err)
	}
}

func TestIsFavoritedFalseWhenMissing(t *testing.T) {
	svc, _, _ := newTestService(t)
	ok, err := svc.IsFavorited(context.Background(), mustID(t), mustID(t))
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestAddRejectsZeroIDs(t *testing.T) {
	svc, _, _ := newTestService(t)
	if err := svc.Add(context.Background(), ID{}, mustID(t)); !errors.Is(err, errZeroID) {
		t.Fatalf("err = %v", err)
	}
}

func newTestService(t *testing.T) (*Service, *MemoryStore, *stubListings) {
	t.Helper()
	store := NewMemoryStore()
	listings := &stubListings{snaps: map[listingcontracts.ID]listingcontracts.ListingSnapshot{}}
	svc, err := NewService(store, listings, func() time.Time { return time.Unix(10, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, listings
}

type stubListings struct {
	snaps map[listingcontracts.ID]listingcontracts.ListingSnapshot
	err   error
}

func (s *stubListings) setPublished(id ID) {
	s.snaps[listingcontracts.ID(id)] = listingcontracts.ListingSnapshot{
		ID:     listingcontracts.ID(id),
		Status: listingcontracts.StatusPublished,
	}
}

func (s *stubListings) GetListingSnapshot(_ context.Context, listingID listingcontracts.ID) (listingcontracts.ListingSnapshot, error) {
	if s.err != nil {
		return listingcontracts.ListingSnapshot{}, s.err
	}
	snap, ok := s.snaps[listingID]
	if !ok {
		return listingcontracts.ListingSnapshot{}, listingcontracts.ErrNotFound
	}
	return snap, nil
}
