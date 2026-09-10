package needs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	bizcontracts "backend/internal/businesses/contracts"
	mdcontracts "backend/internal/masterdata/contracts"
)

func TestListCandidatesOpenNeed(t *testing.T) {
	svc, _, now := mustMatchingService(t)
	owner := mustID(t)
	need := mustOpenNeed(t, svc, now, owner, validContent("Need", ""))
	got, err := svc.ListCandidates(context.Background(), owner, need.ID, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("got = %+v err = %v", got, err)
	}
	if got[0].ServiceTitle != "Eligible" {
		t.Fatalf("got = %+v", got)
	}
}

func TestListCandidatesRejectsDraftAndTerminalAndForeign(t *testing.T) {
	svc, _, now := mustMatchingService(t)
	owner := mustID(t)
	draft, err := svc.Create(context.Background(), owner, validContent("Need", ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListCandidates(context.Background(), owner, draft.ID, 0); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("draft err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	opened, err := svc.Open(context.Background(), owner, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Fulfill(context.Background(), owner, opened.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListCandidates(context.Background(), owner, opened.ID, 0); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("fulfilled err = %v", err)
	}
	if _, err := svc.ListCandidates(context.Background(), mustID(t), opened.ID, 0); !errors.Is(err, errNotFound) {
		t.Fatalf("foreign err = %v", err)
	}
}

func TestListCandidatesUsesNeedRadiusAndCategoryFilter(t *testing.T) {
	biz := NewStubCandidates()
	store := NewMemoryStore()
	clock := &frozenNow{now: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
	cat := mustID(t)
	svc, err := NewService(store, stubCategories{ok: map[mdcontracts.ID]struct{}{mdcontracts.ID(cat): {}}}, biz, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	radius := 5.0
	content := validContent("Need", "")
	content.RadiusKm = &radius
	content.CategoryID = &cat
	need, err := svc.Create(context.Background(), mustID(t), content)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Minute)
	if _, err := svc.Open(context.Background(), need.RequesterUserID, need.ID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.ListCandidates(context.Background(), need.RequesterUserID, need.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if biz.last.RadiusKm != 5 || biz.last.Limit != 3 || biz.last.CategoryID == nil || ID(*biz.last.CategoryID) != cat {
		t.Fatalf("query = %+v", biz.last)
	}
}

func TestListCandidatesDedupesAndClampsLimit(t *testing.T) {
	biz := NewStubCandidates()
	dup := mustCandidate(t, "Same")
	biz.out = []bizcontracts.ServiceCandidate{dup, dup}
	store := NewMemoryStore()
	clock := &frozenNow{now: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(store, stubCategories{}, biz, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	need := mustOpenNeedWith(t, svc, clock, mustID(t), validContent("Need", ""))
	got, err := svc.ListCandidates(context.Background(), need.RequesterUserID, need.ID, 999)
	if err != nil || len(got) != 1 {
		t.Fatalf("got = %+v err = %v", got, err)
	}
	if biz.last.Limit != MaxCandidateLimit {
		t.Fatalf("limit = %d", biz.last.Limit)
	}
}

type stubCandidates struct {
	out  []bizcontracts.ServiceCandidate
	last bizcontracts.CandidateQuery
	err  error
}

func NewStubCandidates() *stubCandidates {
	id, _ := NewID()
	biz, _ := NewID()
	return &stubCandidates{out: []bizcontracts.ServiceCandidate{{
		BusinessID:          bizcontracts.ID(biz),
		BusinessDisplayName: "Kafe",
		ServiceID:           bizcontracts.ID(id),
		ServiceTitle:        "Eligible",
		DistanceKm:          1.2,
	}}}
}

func (s *stubCandidates) FindServiceCandidates(ctx context.Context, query bizcontracts.CandidateQuery) ([]bizcontracts.ServiceCandidate, error) {
	s.last = query
	if s.err != nil {
		return nil, s.err
	}
	return s.out, nil
}

func mustMatchingService(t *testing.T) (*Service, *MemoryStore, *frozenNow) {
	t.Helper()
	store := NewMemoryStore()
	clock := &frozenNow{now: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(store, stubCategories{}, NewStubCandidates(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, clock
}

func mustOpenNeed(t *testing.T, svc *Service, now *frozenNow, owner ID, content Content) Need {
	t.Helper()
	return mustOpenNeedWith(t, svc, now, owner, content)
}

func mustOpenNeedWith(t *testing.T, svc *Service, now *frozenNow, owner ID, content Content) Need {
	t.Helper()
	created, err := svc.Create(context.Background(), owner, content)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	opened, err := svc.Open(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	return opened
}

func mustCandidate(t *testing.T, title string) bizcontracts.ServiceCandidate {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	biz, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return bizcontracts.ServiceCandidate{
		BusinessID:          bizcontracts.ID(biz),
		BusinessDisplayName: "Biz",
		ServiceID:           bizcontracts.ID(id),
		ServiceTitle:        title,
		DistanceKm:          0.5,
	}
}

func TestCandidateDTOHasNoRequesterFields(t *testing.T) {
	c := ServiceCandidate{BusinessDisplayName: "Kafe", ServiceTitle: "Tur"}
	raw := c.BusinessDisplayName + c.ServiceTitle
	if strings.Contains(strings.ToLower(raw), "userid") {
		t.Fatal(raw)
	}
}
