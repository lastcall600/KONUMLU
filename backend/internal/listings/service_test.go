package listings

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/listings/contracts"
	"backend/internal/platform/db"
)

func TestServiceCreateDraft(t *testing.T) {
	svc, store, _ := mustService(t)
	owner := mustID(t)
	got, err := svc.CreateDraft(context.Background(), owner, validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDraft || got.OwnerUserID != owner {
		t.Fatalf("listing = %+v", got)
	}
	stored, err := store.Get(context.Background(), got.ID)
	if err != nil || stored.Title != "Bike" {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	listed, err := svc.ListByOwner(context.Background(), owner)
	if err != nil || len(listed) != 1 || listed[0].ID != got.ID {
		t.Fatalf("list = %+v err = %v", listed, err)
	}
}

func TestServiceReadyArchivePublish(t *testing.T) {
	svc, _, now := mustService(t)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	ready, err := svc.MarkReady(context.Background(), created.ID, created.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Status != StatusReady {
		t.Fatalf("status = %s", ready.Status)
	}
	now.now = now.now.Add(time.Minute)
	published, err := svc.Publish(context.Background(), ready.ID, ready.UpdatedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	if published.Status != StatusPublished {
		t.Fatalf("status = %s", published.Status)
	}
	now.now = now.now.Add(time.Minute)
	archived, err := svc.Archive(context.Background(), published.ID, published.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if archived.Status != StatusArchived {
		t.Fatalf("status = %s", archived.Status)
	}
}

func TestServiceInvalidTransitionAndEligibility(t *testing.T) {
	svc, _, now := mustService(t)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(context.Background(), created.ID, created.UpdatedAt, true); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("draft publish err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	ready, err := svc.MarkReady(context.Background(), created.ID, created.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateDraft(context.Background(), ready.ID, ready.UpdatedAt, validContent(ready.CategoryID)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("update ready err = %v", err)
	}
	if _, err := svc.Publish(context.Background(), ready.ID, ready.UpdatedAt, false); !errors.Is(err, errPublishNotEligible) {
		t.Fatalf("eligibility err = %v", err)
	}
}

func TestServiceGetPublishedVisibility(t *testing.T) {
	svc, store, now := mustService(t)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublished(context.Background(), created.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("draft err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	ready, err := svc.MarkReady(context.Background(), created.ID, created.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublished(context.Background(), ready.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("ready err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	published, err := svc.Publish(context.Background(), ready.ID, ready.UpdatedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetPublished(context.Background(), published.ID)
	if err != nil || got.Status != StatusPublished {
		t.Fatalf("published = %+v err = %v", got, err)
	}
	now.now = now.now.Add(time.Minute)
	archived, err := svc.Archive(context.Background(), published.ID, published.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublished(context.Background(), archived.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("archived err = %v", err)
	}
	pending, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	expected := pending.UpdatedAt
	pending.Status = StatusVerificationPending
	pending.UpdatedAt = expected.Add(time.Second)
	if err := store.Update(context.Background(), pending, expected); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublished(context.Background(), pending.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("verification_pending err = %v", err)
	}
	store.SetFail(db.ErrUnavailable)
	if _, err := svc.GetPublished(context.Background(), published.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("unavailable err = %v", err)
	}
	store.SetFail(nil)
	missing, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublished(context.Background(), missing); !errors.Is(err, errNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestServiceModerationHideIsNotArchive(t *testing.T) {
	svc, _, now := mustService(t)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	ready, err := svc.MarkReady(context.Background(), created.ID, created.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	published, err := svc.Publish(context.Background(), ready.ID, ready.UpdatedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublished(context.Background(), published.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyModerationInput{
		ListingID: contracts.ID(published.ID), State: contracts.ModerationStateRestricted,
	}); err != nil {
		t.Fatal(err)
	}
	hidden, err := svc.Get(context.Background(), published.ID)
	if err != nil || hidden.Status != StatusPublished || hidden.ModerationState != ModerationRestricted || hidden.ArchivedAt != nil {
		t.Fatalf("restricted stored = %+v err=%v", hidden, err)
	}
	if _, err := svc.GetPublished(context.Background(), published.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("restricted public err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyModerationInput{
		ListingID: contracts.ID(published.ID), State: contracts.ModerationStateRestricted,
	}); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyModerationInput{
		ListingID: contracts.ID(published.ID), State: contracts.ModerationStateRemoved,
	}); err != nil {
		t.Fatal(err)
	}
	removed, err := svc.Get(context.Background(), published.ID)
	if err != nil || removed.Status != StatusPublished || removed.ModerationState != ModerationRemoved {
		t.Fatalf("removed stored = %+v err=%v", removed, err)
	}
	if _, err := svc.GetPublished(context.Background(), published.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("removed public err = %v", err)
	}
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyModerationInput{
		ListingID: contracts.ID(published.ID), State: contracts.ModerationStateNone,
	}); !errors.Is(err, contracts.ErrModerationNotEnforced) {
		t.Fatalf("none apply err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	if err := svc.ClearModerationState(context.Background(), contracts.ID(published.ID)); err != nil {
		t.Fatal(err)
	}
	cleared, err := svc.Get(context.Background(), published.ID)
	if err != nil || cleared.Status != StatusPublished || cleared.ModerationState != ModerationNone {
		t.Fatalf("cleared = %+v err=%v", cleared, err)
	}
	if _, err := svc.GetPublished(context.Background(), published.ID); err != nil {
		t.Fatalf("restored public err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	if err := svc.ClearModerationState(context.Background(), contracts.ID(published.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestServiceClearModerationDoesNotUnarchive(t *testing.T) {
	svc, _, now := mustService(t)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	ready, err := svc.MarkReady(context.Background(), created.ID, created.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	published, err := svc.Publish(context.Background(), ready.ID, ready.UpdatedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyModerationInput{
		ListingID: contracts.ID(published.ID), State: contracts.ModerationStateRemoved,
	}); err != nil {
		t.Fatal(err)
	}
	hidden, err := svc.Get(context.Background(), published.ID)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	archived, err := svc.Archive(context.Background(), hidden.ID, hidden.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if err := svc.ClearModerationState(context.Background(), contracts.ID(archived.ID)); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), archived.ID)
	if err != nil || got.Status != StatusArchived || got.ModerationState != ModerationNone || got.ArchivedAt == nil {
		t.Fatalf("archived restore = %+v err=%v", got, err)
	}
	if _, err := svc.GetPublished(context.Background(), archived.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("archived public err = %v", err)
	}
}

func TestServiceApplyModerationFailureLeavesPublic(t *testing.T) {
	svc, store, now := mustService(t)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	ready, err := svc.MarkReady(context.Background(), created.ID, created.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	published, err := svc.Publish(context.Background(), ready.ID, ready.UpdatedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	store.SetFail(db.ErrUnavailable)
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyModerationInput{
		ListingID: contracts.ID(published.ID), State: contracts.ModerationStateRestricted,
	}); !errors.Is(err, contracts.ErrUnavailable) {
		t.Fatalf("apply err = %v", err)
	}
	store.SetFail(nil)
	got, err := svc.GetPublished(context.Background(), published.ID)
	if err != nil || got.ModerationState.Normalized() != ModerationNone {
		t.Fatalf("still public = %+v err=%v", got, err)
	}
}

func TestServiceOptimisticConflict(t *testing.T) {
	svc, _, now := mustService(t)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	stale := created.UpdatedAt.Add(-time.Second)
	now.now = now.now.Add(time.Minute)
	if _, err := svc.MarkReady(context.Background(), created.ID, stale); !errors.Is(err, errConflict) {
		t.Fatalf("stale err = %v", err)
	}
}

func TestServicePersistenceErrorMapping(t *testing.T) {
	store := NewMemoryStore()
	svc, err := NewService(store, defaultPublishedForms(), func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	store.SetFail(db.ErrUnavailable)
	if _, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t))); !errors.Is(err, errUnavailable) {
		t.Fatalf("unavailable err = %v", err)
	}
	store.SetFail(db.ErrNoRows)
	if _, err := svc.Get(context.Background(), mustID(t)); !errors.Is(err, errNotFound) {
		t.Fatalf("not found err = %v", err)
	}
	store.SetFail(db.ErrConflict)
	if _, err := svc.ListByOwner(context.Background(), mustID(t)); !errors.Is(err, errConflict) {
		t.Fatalf("conflict err = %v", err)
	}
	if _, err := NewService(nil, nil, nil); !errors.Is(err, errStoreRequired) {
		t.Fatalf("nil store err = %v", err)
	}
}

type frozenNow struct {
	now time.Time
}

func mustService(t *testing.T) (*Service, *MemoryStore, *frozenNow) {
	t.Helper()
	clock := &frozenNow{now: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)}
	store := NewMemoryStore()
	svc, err := NewService(store, defaultPublishedForms(), func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, clock
}
