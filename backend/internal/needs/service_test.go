package needs

import (
	"context"
	"errors"
	"testing"
	"time"

	mdcontracts "backend/internal/masterdata/contracts"
	"backend/internal/needs/contracts"
)

func TestServiceCreateRequesterAndList(t *testing.T) {
	svc, store, now := mustService(t)
	requester := mustID(t)
	got, err := svc.Create(context.Background(), requester, validContent("İhtiyaç", "Açıklama"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDraft || got.RequesterUserID != requester {
		t.Fatalf("need = %+v", got)
	}
	stored, err := store.Get(context.Background(), got.ID)
	if err != nil || stored.Title != "İhtiyaç" {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	now.now = now.now.Add(time.Minute)
	second, err := svc.Create(context.Background(), requester, validContent("Second", ""))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := svc.ListOwned(context.Background(), requester)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list = %+v err = %v", listed, err)
	}
	if listed[0].ID != second.ID || listed[1].ID != got.ID {
		t.Fatalf("order = %s then %s", listed[0].ID, listed[1].ID)
	}
	if _, err := svc.GetOwned(context.Background(), mustID(t), got.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("foreign get err = %v", err)
	}
}

func TestServiceRejectsUnknownCategory(t *testing.T) {
	svc, _, _ := mustService(t)
	cat := mustID(t)
	content := validContent("Need", "")
	content.CategoryID = &cat
	if _, err := svc.Create(context.Background(), mustID(t), content); !errors.Is(err, errInvalidCategory) {
		t.Fatalf("err = %v", err)
	}
}

func TestServiceAcceptsPublishedCategory(t *testing.T) {
	svc, _, _ := mustService(t)
	cat := mustID(t)
	svc.categories = stubCategories{ok: map[mdcontracts.ID]struct{}{mdcontracts.ID(cat): {}}}
	content := validContent("Need", "")
	content.CategoryID = &cat
	got, err := svc.Create(context.Background(), mustID(t), content)
	if err != nil {
		t.Fatal(err)
	}
	if got.CategoryID == nil || *got.CategoryID != cat {
		t.Fatalf("category = %v", got.CategoryID)
	}
}

func TestServiceLifecycleAndTerminalImmutable(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	created, err := svc.Create(context.Background(), owner, validContent("Need", ""))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	opened, err := svc.Open(context.Background(), owner, created.ID)
	if err != nil || opened.Status != StatusOpen {
		t.Fatalf("open = %+v err = %v", opened, err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Open(context.Background(), owner, created.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("double open err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	fulfilled, err := svc.Fulfill(context.Background(), owner, created.ID)
	if err != nil || fulfilled.Status != StatusFulfilled {
		t.Fatalf("fulfill = %+v err = %v", fulfilled, err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Update(context.Background(), owner, created.ID, validContent("Nope", "")); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("update terminal err = %v", err)
	}
	if _, err := svc.Cancel(context.Background(), owner, created.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("cancel terminal err = %v", err)
	}
}

func TestServiceExpireIfDue(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	expires := now.now.Add(time.Hour)
	content := validContent("Need", "")
	content.ExpiresAt = &expires
	created, err := svc.Create(context.Background(), owner, content)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExpireIfDue(context.Background(), created.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("early err = %v", err)
	}
	now.now = now.now.Add(2 * time.Hour)
	expired, err := svc.ExpireIfDue(context.Background(), created.ID)
	if err != nil || expired.Status != StatusExpired {
		t.Fatalf("expire = %+v err = %v", expired, err)
	}
}

func TestServiceUpdateDraft(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	created, err := svc.Create(context.Background(), owner, validContent("Need", ""))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	updated, err := svc.Update(context.Background(), owner, created.ID, validContent("Updated", "Yeni"))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Updated" || updated.Status != StatusDraft {
		t.Fatalf("updated = %+v", updated)
	}
}

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time { return f.now }

func mustService(t *testing.T) (*Service, *MemoryStore, *frozenNow) {
	t.Helper()
	store := NewMemoryStore()
	clock := &frozenNow{now: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(store, stubCategories{}, nil, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, clock
}

type stubCategories struct {
	ok  map[mdcontracts.ID]struct{}
	err error
}

func (s stubCategories) RequirePublished(ctx context.Context, categoryID mdcontracts.ID) error {
	if s.err != nil {
		return s.err
	}
	if categoryID.IsZero() {
		return mdcontracts.ErrZeroID
	}
	if _, ok := s.ok[categoryID]; !ok {
		return mdcontracts.ErrNotFound
	}
	return nil
}

func TestNeedLookupContract(t *testing.T) {
	svc, _, _ := mustService(t)
	requester := mustID(t)
	need, err := svc.Create(context.Background(), requester, validContent("Need", ""))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := svc.GetNeed(context.Background(), contracts.ID(need.ID))
	if err != nil || ref.RequesterUserID != contracts.ID(requester) || ref.Title != "Need" {
		t.Fatalf("ref = %+v err = %v", ref, err)
	}
	if err := svc.AssertOwnedBy(context.Background(), contracts.ID(need.ID), contracts.ID(requester)); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssertOwnedBy(context.Background(), contracts.ID(need.ID), contracts.ID(mustID(t))); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("foreign err = %v", err)
	}
}
