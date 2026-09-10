package businesses

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/businesses/contracts"
)

func TestServiceCreateOwnerAndUniqueness(t *testing.T) {
	svc, store, _ := mustService(t)
	owner := mustID(t)
	got, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Kafe"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDraft || got.OwnerUserID != owner {
		t.Fatalf("profile = %+v", got)
	}
	stored, err := store.Get(context.Background(), got.ID)
	if err != nil || stored.DisplayName != "Kafe" {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	if _, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Other"}); !errors.Is(err, errConflict) {
		t.Fatalf("second create err = %v", err)
	}
	mine, err := svc.GetMine(context.Background(), owner)
	if err != nil || mine.ID != got.ID {
		t.Fatalf("mine = %+v err = %v", mine, err)
	}
}

func TestServiceGetOwnedAndPublic(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	other := mustID(t)
	created, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Kafe", Description: "Sahil"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetOwned(context.Background(), other, created.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("foreign get err = %v", err)
	}
	if _, err := svc.GetPublic(context.Background(), created.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("draft public err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	active, err := svc.Activate(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := svc.GetPublic(context.Background(), active.ID)
	if err != nil || pub.Status != StatusActive {
		t.Fatalf("public = %+v err = %v", pub, err)
	}
	now.now = now.now.Add(time.Minute)
	closed, err := svc.Close(context.Background(), owner, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublic(context.Background(), closed.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("closed public err = %v", err)
	}
}

func TestServiceInvalidTransitions(t *testing.T) {
	svc, store, now := mustService(t)
	owner := mustID(t)
	created, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Kafe"})
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.Activate(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Activate(context.Background(), owner, created.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("active again err = %v", err)
	}
	suspended := active
	suspended.Status = StatusSuspended
	suspended.UpdatedAt = now.now
	if err := store.Update(context.Background(), suspended, active.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Activate(context.Background(), owner, created.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("activate suspended err = %v", err)
	}
	if _, err := svc.Close(context.Background(), owner, created.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("close suspended err = %v", err)
	}
}

func TestServiceLookupContract(t *testing.T) {
	svc, _, _ := mustService(t)
	owner := mustID(t)
	created, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Kafe"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := svc.GetBusiness(context.Background(), contracts.ID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if ref.OwnerUserID != contracts.ID(owner) || ref.Status != string(StatusDraft) {
		t.Fatalf("ref = %+v", ref)
	}
	if err := svc.AssertOwnedBy(context.Background(), contracts.ID(created.ID), contracts.ID(owner)); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssertOwnedBy(context.Background(), contracts.ID(created.ID), contracts.ID(mustID(t))); !errors.Is(err, contracts.ErrForbidden) {
		t.Fatalf("foreign assert err = %v", err)
	}
}

func TestServiceMineMissing(t *testing.T) {
	svc, _, _ := mustService(t)
	if _, err := svc.GetMine(context.Background(), mustID(t)); !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v", err)
	}
}

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time {
	return f.now
}

func mustService(t *testing.T) (*Service, *MemoryStore, *frozenNow) {
	t.Helper()
	store := NewMemoryStore()
	clock := &frozenNow{now: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(store, nil, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, clock
}
