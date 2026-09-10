package reviews

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/platform/outbox"
	verifiedcontracts "backend/internal/verified/contracts"
)

func TestCompletedListingInspectionIsEligible(t *testing.T) {
	env := newServiceEnv(t)
	interaction := env.putInspection(t, env.reviewer, env.now.Add(-time.Hour))
	got, err := env.svc.Eligibility(context.Background(), env.reviewer, interaction)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Eligible || got.AlreadyReviewed || got.ExpiresAt == nil {
		t.Fatalf("eligibility = %+v", got)
	}
	row, err := env.svc.Create(context.Background(), env.reviewer, CreateInput{
		VerifiedInteractionID: interaction,
		ListingAccuracy:       5,
		ProviderService:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.ListingAccuracy != 5 || row.ProviderService != 1 {
		t.Fatalf("ratings mixed: %+v", row)
	}
}

func TestWrongRequesterIsPrivacySafeNotFound(t *testing.T) {
	env := newServiceEnv(t)
	interaction := env.putInspection(t, env.reviewer, env.now.Add(-time.Hour))
	other := mustID(t)
	_, err := env.svc.Eligibility(context.Background(), other, interaction)
	if !errors.Is(err, errNotFound) {
		t.Fatalf("eligibility err = %v", err)
	}
	_, err = env.svc.Create(context.Background(), other, CreateInput{
		VerifiedInteractionID: interaction,
		ListingAccuracy:       3,
		ProviderService:       3,
	})
	if !errors.Is(err, errNotFound) {
		t.Fatalf("create err = %v", err)
	}
}

func TestExpiredWindowNotEligible(t *testing.T) {
	env := newServiceEnv(t)
	verifiedAt := env.now.Add(-25 * time.Hour)
	interaction := env.putInspection(t, env.reviewer, verifiedAt)
	got, err := env.svc.Eligibility(context.Background(), env.reviewer, interaction)
	if err != nil {
		t.Fatal(err)
	}
	if got.Eligible {
		t.Fatalf("expired still eligible: %+v", got)
	}
	_, err = env.svc.Create(context.Background(), env.reviewer, CreateInput{
		VerifiedInteractionID: interaction,
		ListingAccuracy:       4,
		ProviderService:       4,
	})
	if !errors.Is(err, errNotEligible) {
		t.Fatalf("create err = %v", err)
	}
}

func TestOneReviewPerInteractionConflict(t *testing.T) {
	env := newServiceEnv(t)
	interaction := env.putInspection(t, env.reviewer, env.now.Add(-time.Hour))
	in := CreateInput{VerifiedInteractionID: interaction, ListingAccuracy: 2, ProviderService: 5}
	if _, err := env.svc.Create(context.Background(), env.reviewer, in); err != nil {
		t.Fatal(err)
	}
	got, err := env.svc.Eligibility(context.Background(), env.reviewer, interaction)
	if err != nil || got.Eligible || !got.AlreadyReviewed {
		t.Fatalf("eligibility = %+v err=%v", got, err)
	}
	if _, err := env.svc.Create(context.Background(), env.reviewer, in); !errors.Is(err, errConflict) {
		t.Fatalf("duplicate err = %v", err)
	}
	rows, err := env.svc.ListMine(context.Background(), env.reviewer)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %+v err=%v", rows, err)
	}
	if len(env.outbox.Events) != 1 {
		t.Fatalf("events = %d", len(env.outbox.Events))
	}
}

func TestInvalidRatingAndBodyRules(t *testing.T) {
	env := newServiceEnv(t)
	interaction := env.putInspection(t, env.reviewer, env.now.Add(-time.Hour))
	_, err := env.svc.Create(context.Background(), env.reviewer, CreateInput{
		VerifiedInteractionID: interaction,
		ListingAccuracy:       0,
		ProviderService:       3,
	})
	if !errors.Is(err, errInvalidRating) {
		t.Fatalf("invalid rating err = %v", err)
	}
	_, err = env.svc.Create(context.Background(), env.reviewer, CreateInput{
		VerifiedInteractionID: interaction,
		Body:                  strings.Repeat("a", MaxBodyBytes+1),
		ListingAccuracy:       3,
		ProviderService:       3,
	})
	if !errors.Is(err, errInvalidBody) {
		t.Fatalf("oversize err = %v", err)
	}
	row, err := env.svc.Create(context.Background(), env.reviewer, CreateInput{
		VerifiedInteractionID: interaction,
		Body:                  "  fine  ",
		ListingAccuracy:       3,
		ProviderService:       4,
	})
	if err != nil || row.Body == nil || *row.Body != "fine" {
		t.Fatalf("optional body = %+v err=%v", row, err)
	}
}

func TestVerifiedCreatedEventHasSeparateRatingsAndNoBody(t *testing.T) {
	env := newServiceEnv(t)
	interaction := env.putInspection(t, env.reviewer, env.now.Add(-time.Hour))
	_, err := env.svc.Create(context.Background(), env.reviewer, CreateInput{
		VerifiedInteractionID: interaction,
		Body:                  "do not emit",
		ListingAccuracy:       5,
		ProviderService:       1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(env.outbox.Events) != 1 {
		t.Fatalf("events = %d", len(env.outbox.Events))
	}
	ev := env.outbox.Events[0]
	if ev.EventType != EventTypeVerifiedCreated || ev.EventVersion != EventVersion {
		t.Fatalf("event = %+v", ev)
	}
	if strings.Contains(string(ev.Payload), "do not emit") {
		t.Fatalf("body in payload: %s", ev.Payload)
	}
	payload, err := DecodeVerifiedCreated(ev.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if payload.ListingAccuracy != 5 || payload.ProviderService != 1 {
		t.Fatalf("payload ratings = %+v", payload)
	}
	var raw map[string]any
	if err := json.Unmarshal(ev.Payload, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["listing_accuracy"]; !ok {
		t.Fatal("missing listing_accuracy")
	}
	if _, ok := raw["provider_service"]; !ok {
		t.Fatal("missing provider_service")
	}
	if _, ok := raw["score"]; ok {
		t.Fatal("combined score present")
	}
}

func TestListPublicPublishedListingNewestFirstOmitsIdentities(t *testing.T) {
	env := newServiceEnv(t)
	listing := mustID(t)
	env.listings.publish(listing)
	older := env.putInspectionOn(t, env.reviewer, listing, env.now.Add(-2*time.Hour))
	if _, err := env.svc.Create(context.Background(), env.reviewer, CreateInput{
		VerifiedInteractionID: older,
		Body:                  "older body",
		ListingAccuracy:       2,
		ProviderService:       3,
	}); err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Hour)
	newer := env.putInspectionOn(t, env.reviewer, listing, env.now.Add(-time.Hour))
	if _, err := env.svc.Create(context.Background(), env.reviewer, CreateInput{
		VerifiedInteractionID: newer,
		Body:                  "newer body",
		ListingAccuracy:       5,
		ProviderService:       4,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := env.svc.ListPublicForListing(context.Background(), PublicListQuery{ListingID: listing})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Reviews) != 2 {
		t.Fatalf("len = %d", len(got.Reviews))
	}
	if got.Reviews[0].Body == nil || *got.Reviews[0].Body != "newer body" || got.Reviews[1].Body == nil || *got.Reviews[1].Body != "older body" {
		t.Fatalf("order = %+v", got.Reviews)
	}
	if got.Reviews[0].ListingAccuracy != 5 || got.Reviews[0].ProviderService != 4 {
		t.Fatalf("ratings = %+v", got.Reviews[0])
	}
	if got.Reviews[0].ID.IsZero() {
		t.Fatal("missing review id")
	}
	if got.NextCursor != "" {
		t.Fatalf("unexpected cursor = %s", got.NextCursor)
	}
}

func TestListPublicNonPublishedIsNotFound(t *testing.T) {
	env := newServiceEnv(t)
	listing := mustID(t)
	env.listings.rows[listingcontracts.ID(listing)] = listingcontracts.ListingRef{
		ID:          listingcontracts.ID(listing),
		OwnerUserID: listingcontracts.ID(mustID(t)),
		Status:      "draft",
	}
	_, err := env.svc.ListPublicForListing(context.Background(), PublicListQuery{ListingID: listing})
	if !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestListPublicEmptyPublishedListing(t *testing.T) {
	env := newServiceEnv(t)
	listing := mustID(t)
	env.listings.publish(listing)
	got, err := env.svc.ListPublicForListing(context.Background(), PublicListQuery{ListingID: listing})
	if err != nil {
		t.Fatal(err)
	}
	if got.Reviews == nil || len(got.Reviews) != 0 || got.NextCursor != "" {
		t.Fatalf("got = %+v", got)
	}
}

func TestListPublicUnavailableStore(t *testing.T) {
	env := newServiceEnv(t)
	listing := mustID(t)
	env.listings.publish(listing)
	env.store.SetFail(errUnavailable)
	_, err := env.svc.ListPublicForListing(context.Background(), PublicListQuery{ListingID: listing})
	if !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestListPublicCursorPagesAndBoundedLimit(t *testing.T) {
	env := newServiceEnv(t)
	listing := mustID(t)
	env.listings.publish(listing)
	bodies := []string{"a", "b", "c"}
	for i, body := range bodies {
		env.now = env.now.Add(time.Duration(i+1) * time.Hour)
		interaction := env.putInspectionOn(t, env.reviewer, listing, env.now.Add(-time.Minute))
		if _, err := env.svc.Create(context.Background(), env.reviewer, CreateInput{
			VerifiedInteractionID: interaction,
			Body:                  body,
			ListingAccuracy:       3,
			ProviderService:       3,
		}); err != nil {
			t.Fatal(err)
		}
	}
	page1, err := env.svc.ListPublicForListing(context.Background(), PublicListQuery{ListingID: listing, Limit: 2})
	if err != nil || len(page1.Reviews) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v err=%v", page1, err)
	}
	if page1.Reviews[0].Body == nil || *page1.Reviews[0].Body != "c" || page1.Reviews[1].Body == nil || *page1.Reviews[1].Body != "b" {
		t.Fatalf("page1 order = %+v", page1.Reviews)
	}
	page2, err := env.svc.ListPublicForListing(context.Background(), PublicListQuery{ListingID: listing, Limit: 2, Cursor: page1.NextCursor})
	if err != nil || len(page2.Reviews) != 1 || page2.NextCursor != "" {
		t.Fatalf("page2 = %+v err=%v", page2, err)
	}
	if page2.Reviews[0].Body == nil || *page2.Reviews[0].Body != "a" {
		t.Fatalf("page2 = %+v", page2.Reviews)
	}
	if _, err := env.svc.ListPublicForListing(context.Background(), PublicListQuery{ListingID: listing, Cursor: "not-a-cursor"}); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("invalid cursor err = %v", err)
	}
	if _, err := env.svc.ListPublicForListing(context.Background(), PublicListQuery{ListingID: listing, Limit: MaxPublicReviews + 1}); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("limit err = %v", err)
	}
}

type stubVerified struct {
	rows map[verifiedcontracts.ID]verifiedcontracts.InteractionRef
}

func (s *stubVerified) GetInteraction(_ context.Context, id verifiedcontracts.ID) (verifiedcontracts.InteractionRef, error) {
	row, ok := s.rows[id]
	if !ok {
		return verifiedcontracts.InteractionRef{}, verifiedcontracts.ErrNotFound
	}
	return row, nil
}

type memoryEnqueuer struct {
	Events []outbox.Event
}

func (e *memoryEnqueuer) Enqueue(ctx context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	if err := ctx.Err(); err != nil {
		return outbox.Event{}, err
	}
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	ev := outbox.Event{
		ID:           id,
		EventType:    in.EventType,
		EventVersion: in.EventVersion,
		Payload:      append([]byte(nil), in.Payload...),
		CreatedAt:    time.Now().UTC(),
		AvailableAt:  time.Now().UTC(),
	}
	e.Events = append(e.Events, ev)
	return ev, nil
}

type stubListings struct {
	rows map[listingcontracts.ID]listingcontracts.ListingRef
}

func (s *stubListings) publish(listingID ID) {
	s.rows[listingcontracts.ID(listingID)] = listingcontracts.ListingRef{
		ID:          listingcontracts.ID(listingID),
		OwnerUserID: listingcontracts.ID(listingID),
		Status:      listingcontracts.StatusPublished,
	}
}

func (s *stubListings) ResolveListingOwner(_ context.Context, listingID listingcontracts.ID) (listingcontracts.ListingRef, error) {
	row, ok := s.rows[listingID]
	if !ok {
		return listingcontracts.ListingRef{}, listingcontracts.ErrNotFound
	}
	return row, nil
}

func (s *stubListings) AssertListingOwnedBy(ctx context.Context, listingID, userID listingcontracts.ID) error {
	ref, err := s.ResolveListingOwner(ctx, listingID)
	if err != nil {
		return err
	}
	if ref.OwnerUserID != userID {
		return listingcontracts.ErrForbidden
	}
	return nil
}

type serviceEnv struct {
	svc      *Service
	store    *MemoryStore
	verified *stubVerified
	listings *stubListings
	outbox   *memoryEnqueuer
	reviewer ID
	now      time.Time
}

func newServiceEnv(t *testing.T) *serviceEnv {
	t.Helper()
	reviewer := mustID(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	store := NewMemoryStore()
	verified := &stubVerified{rows: map[verifiedcontracts.ID]verifiedcontracts.InteractionRef{}}
	listings := &stubListings{rows: map[listingcontracts.ID]listingcontracts.ListingRef{}}
	enqueuer := &memoryEnqueuer{}
	env := &serviceEnv{store: store, verified: verified, listings: listings, outbox: enqueuer, reviewer: reviewer, now: now}
	svc, err := NewService(store, verified, listings, enqueuer, DefaultPolicy(), func() time.Time { return env.now })
	if err != nil {
		t.Fatal(err)
	}
	env.svc = svc
	return env
}

func (e *serviceEnv) putInspection(t *testing.T, requester ID, verifiedAt time.Time) ID {
	t.Helper()
	return e.putInspectionOn(t, requester, mustID(t), verifiedAt)
}

func (e *serviceEnv) putInspectionOn(t *testing.T, requester, listing ID, verifiedAt time.Time) ID {
	t.Helper()
	id := mustID(t)
	e.verified.rows[verifiedcontracts.ID(id)] = verifiedcontracts.InteractionRef{
		ID:              verifiedcontracts.ID(id),
		ListingID:       verifiedcontracts.ID(listing),
		RequesterUserID: verifiedcontracts.ID(requester),
		ProviderUserID:  verifiedcontracts.ID(mustID(t)),
		InteractionType: verifiedcontracts.InteractionTypeListingInspection,
		VerifiedAt:      verifiedAt,
	}
	return id
}
