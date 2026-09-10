package reviewaggregates

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/outbox"
	reviewscontracts "backend/internal/reviews/contracts"
)

func TestFirstReviewCreatesListingAggregate(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	listing, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustEvent(t, listing, provider, 5, 2, at, nil)); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetListingAccuracy(context.Background(), listing)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewCount != 1 || got.RatingSum != 5 {
		t.Fatalf("listing = %+v", got)
	}
	if avg := got.AverageNumber(); avg == nil || avg.String() != "5" {
		t.Fatalf("listing average = %v", avg)
	}
}

func TestFirstReviewCreatesProviderAggregate(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	listing, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustEvent(t, listing, provider, 5, 2, at, nil)); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetProviderService(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewCount != 1 || got.RatingSum != 2 {
		t.Fatalf("provider = %+v", got)
	}
	if avg := got.AverageNumber(); avg == nil || avg.String() != "2" {
		t.Fatalf("provider average = %v", avg)
	}
}

func TestSecondReviewUpdatesCountSumAverage(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	listing, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustEvent(t, listing, provider, 4, 2, at, nil)); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), mustEvent(t, listing, provider, 5, 3, at.Add(time.Hour), nil)); err != nil {
		t.Fatal(err)
	}
	listingGot, err := store.GetListingAccuracy(context.Background(), listing)
	if err != nil {
		t.Fatal(err)
	}
	if listingGot.ReviewCount != 2 || listingGot.RatingSum != 9 {
		t.Fatalf("listing = %+v", listingGot)
	}
	if avg := listingGot.AverageNumber(); avg == nil || avg.String() != "4.5" {
		t.Fatalf("listing average = %v", avg)
	}
	providerGot, err := store.GetProviderService(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if providerGot.ReviewCount != 2 || providerGot.RatingSum != 5 {
		t.Fatalf("provider = %+v", providerGot)
	}
	if avg := providerGot.AverageNumber(); avg == nil || avg.String() != "2.5" {
		t.Fatalf("provider average = %v", avg)
	}
}

func TestReplaySameEventNoOp(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	listing, provider := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	event := mustEvent(t, listing, provider, 5, 1, at, nil)
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	listingGot, err := store.GetListingAccuracy(context.Background(), listing)
	if err != nil {
		t.Fatal(err)
	}
	if listingGot.ReviewCount != 1 || listingGot.RatingSum != 5 {
		t.Fatalf("listing = %+v", listingGot)
	}
	providerGot, err := store.GetProviderService(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if providerGot.ReviewCount != 1 || providerGot.RatingSum != 1 {
		t.Fatalf("provider = %+v", providerGot)
	}
}

func TestListingAccuracyAndProviderServiceRemainSeparate(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	listingA, listingB := mustID(t), mustID(t)
	providerA, providerB := mustID(t), mustID(t)
	at := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	if err := h.Handle(context.Background(), mustEvent(t, listingA, providerA, 5, 1, at, nil)); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), mustEvent(t, listingB, providerA, 3, 4, at.Add(time.Minute), nil)); err != nil {
		t.Fatal(err)
	}
	a, err := store.GetListingAccuracy(context.Background(), listingA)
	if err != nil {
		t.Fatal(err)
	}
	if a.ReviewCount != 1 || a.RatingSum != 5 {
		t.Fatalf("listing A mixed with other ratings: %+v", a)
	}
	b, err := store.GetListingAccuracy(context.Background(), listingB)
	if err != nil {
		t.Fatal(err)
	}
	if b.ReviewCount != 1 || b.RatingSum != 3 {
		t.Fatalf("listing B = %+v", b)
	}
	provA, err := store.GetProviderService(context.Background(), providerA)
	if err != nil {
		t.Fatal(err)
	}
	if provA.ReviewCount != 2 || provA.RatingSum != 5 {
		t.Fatalf("provider A should only sum provider_service: %+v", provA)
	}
	if _, err := store.GetProviderService(context.Background(), providerB); !errors.Is(err, errNotFound) {
		t.Fatalf("provider B should stay empty: %v", err)
	}
	if _, err := store.GetListingAccuracy(context.Background(), listingA); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedEventFailsSafely(t *testing.T) {
	p, store := mustProjector(t)
	h := mustHandler(t, p)
	eid, err := outbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	event := outbox.Event{
		ID:           eid,
		EventType:    reviewscontracts.EventTypeVerifiedCreated,
		EventVersion: reviewscontracts.EventVersion,
		Payload:      json.RawMessage(`{"listing_id":"not-a-uuid"}`),
		CreatedAt:    time.Now().UTC(),
	}
	if err := h.Handle(context.Background(), event); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("err = %v", err)
	}
	if _, err := store.GetListingAccuracy(context.Background(), mustID(t)); !errors.Is(err, errNotFound) {
		t.Fatalf("unexpected listing err = %v", err)
	}
	if err := h.Handle(context.Background(), outbox.Event{
		ID:           eid,
		EventType:    reviewscontracts.EventTypeVerifiedCreated,
		EventVersion: reviewscontracts.EventVersion,
		Payload:      json.RawMessage(`not-json`),
		CreatedAt:    time.Now().UTC(),
	}); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("bad json err = %v", err)
	}
}

func TestZeroSummaryWhenMissing(t *testing.T) {
	p, _ := mustProjector(t)
	svc, err := NewService(p)
	if err != nil {
		t.Fatal(err)
	}
	listing := mustID(t)
	got, err := svc.ListingAccuracy(context.Background(), listing)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewCount != 0 || got.RatingSum != 0 || got.AverageNumber() != nil {
		t.Fatalf("got = %+v", got)
	}
}

func mustProjector(t *testing.T) (*Projector, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	p, err := NewProjector(store)
	if err != nil {
		t.Fatal(err)
	}
	return p, store
}

func mustHandler(t *testing.T, p *Projector) *ProjectionHandler {
	t.Helper()
	h, err := NewProjectionHandler(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustEvent(t *testing.T, listing, provider ID, listingAccuracy, providerService int, at time.Time, eventID *outbox.ID) outbox.Event {
	t.Helper()
	payload, err := json.Marshal(reviewscontracts.VerifiedCreatedPayload{
		ReviewID:              mustID(t).String(),
		VerifiedInteractionID: mustID(t).String(),
		ListingID:             listing.String(),
		ReviewerUserID:        mustID(t).String(),
		ProviderUserID:        provider.String(),
		ListingAccuracy:       listingAccuracy,
		ProviderService:       providerService,
		CreatedAt:             at.UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	var eid outbox.ID
	if eventID != nil {
		eid = *eventID
	} else {
		eid, err = outbox.NewID()
		if err != nil {
			t.Fatal(err)
		}
	}
	return outbox.Event{
		ID:           eid,
		EventType:    reviewscontracts.EventTypeVerifiedCreated,
		EventVersion: reviewscontracts.EventVersion,
		Payload:      payload,
		CreatedAt:    at,
	}
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
