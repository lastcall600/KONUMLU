package moderation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/platform/outbox"
)

func TestCreateListingReport(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	row, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: listing, ReasonCode: ReasonSpam, Description: "  ads  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.ReporterUserID != env.reporter || row.TargetID != listing || row.Status != StatusSubmitted {
		t.Fatalf("row = %+v", row)
	}
	if row.Description == nil || *row.Description != "ads" {
		t.Fatalf("description = %v", row.Description)
	}
}

func TestCreatePublicProfileReport(t *testing.T) {
	env := newServiceEnv(t)
	profile := env.putProfile(t, env.other)
	row, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetPublicProfile, TargetID: profile, ReasonCode: ReasonImpersonation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.TargetType != TargetPublicProfile || row.TargetID != profile {
		t.Fatalf("row = %+v", row)
	}
}

func TestInvalidTargetAndReasonRejected(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	_, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: "message", TargetID: listing, ReasonCode: ReasonSpam,
	})
	if !errors.Is(err, errInvalidTarget) {
		t.Fatalf("target err = %v", err)
	}
	_, err = env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: listing, ReasonCode: "custom",
	})
	if !errors.Is(err, errInvalidReason) {
		t.Fatalf("reason err = %v", err)
	}
	missing := mustID(t)
	_, err = env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: missing, ReasonCode: ReasonSpam,
	})
	if !errors.Is(err, errNotFound) {
		t.Fatalf("missing listing err = %v", err)
	}
	draft := mustID(t)
	env.listings.rows[listingcontracts.ID(draft)] = listingcontracts.ListingRef{
		ID: listingcontracts.ID(draft), OwnerUserID: listingcontracts.ID(env.other), Status: "draft",
	}
	_, err = env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: draft, ReasonCode: ReasonSpam,
	})
	if !errors.Is(err, errNotFound) {
		t.Fatalf("draft listing err = %v", err)
	}
	_, err = env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetPublicProfile, TargetID: mustID(t), ReasonCode: ReasonOther,
	})
	if !errors.Is(err, errNotFound) {
		t.Fatalf("missing profile err = %v", err)
	}
}

func TestSelfReportRejectedWhenOwnerResolved(t *testing.T) {
	env := newServiceEnv(t)
	ownListing := env.publishListing(t, env.reporter)
	_, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: ownListing, ReasonCode: ReasonSpam,
	})
	if !errors.Is(err, errSelfReport) {
		t.Fatalf("listing self err = %v", err)
	}
	ownProfile := env.putProfile(t, env.reporter)
	_, err = env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetPublicProfile, TargetID: ownProfile, ReasonCode: ReasonHarassment,
	})
	if !errors.Is(err, errSelfReport) {
		t.Fatalf("profile self err = %v", err)
	}
}

func TestDuplicateIdenticalReportConflict(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	in := CreateInput{TargetType: TargetListing, TargetID: listing, ReasonCode: ReasonScamOrFraud}
	if _, err := env.svc.Create(context.Background(), env.reporter, in); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Create(context.Background(), env.reporter, in); !errors.Is(err, errConflict) {
		t.Fatalf("duplicate err = %v", err)
	}
	if _, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: listing, ReasonCode: ReasonSpam,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestClosedDuplicateInsideWindowConflictsOutsideWindowAllowed(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	in := CreateInput{TargetType: TargetListing, TargetID: listing, ReasonCode: ReasonOther}
	closed := Report{
		ID: mustID(t), ReporterUserID: env.reporter, TargetType: TargetListing, TargetID: listing,
		ReasonCode: ReasonOther, Status: StatusClosed, CreatedAt: env.now, UpdatedAt: env.now,
	}
	if err := env.store.Insert(context.Background(), closed); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Create(context.Background(), env.reporter, in); !errors.Is(err, errConflict) {
		t.Fatalf("closed inside window err = %v", err)
	}
	env.now = env.now.Add(DefaultDuplicateWindow + time.Minute)
	if _, err := env.svc.Create(context.Background(), env.reporter, in); err != nil {
		t.Fatalf("closed outside window err = %v", err)
	}
}

func TestOversizeDescriptionRejected(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	_, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: listing, ReasonCode: ReasonSpam,
		Description: strings.Repeat("a", MaxDescriptionBytes+1),
	})
	if !errors.Is(err, errInvalidBody) {
		t.Fatalf("oversize err = %v", err)
	}
}

func TestListMineOwnership(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	if _, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: listing, ReasonCode: ReasonSpam,
	}); err != nil {
		t.Fatal(err)
	}
	mine, err := env.svc.ListMine(context.Background(), env.reporter)
	if err != nil || len(mine) != 1 {
		t.Fatalf("mine = %+v err=%v", mine, err)
	}
	other, err := env.svc.ListMine(context.Background(), env.other)
	if err != nil || len(other) != 0 {
		t.Fatalf("other = %+v err=%v", other, err)
	}
}

func TestListQueueSubmittedNewestBoundedAndFilters(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	profile := env.putProfile(t, env.other)
	first, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: listing, ReasonCode: ReasonSpam,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Minute)
	second, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetPublicProfile, TargetID: profile, ReasonCode: ReasonHarassment,
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := env.svc.ListQueue(context.Background(), QueueQuery{Limit: 1, Order: QueueNewest})
	if err != nil || len(page.Reports) != 1 || page.Reports[0].ID != second.ID || page.NextCursor == "" {
		t.Fatalf("newest page = %+v err=%v", page, err)
	}
	page2, err := env.svc.ListQueue(context.Background(), QueueQuery{Limit: 1, Order: QueueNewest, Cursor: page.NextCursor})
	if err != nil || len(page2.Reports) != 1 || page2.Reports[0].ID != first.ID {
		t.Fatalf("newest page2 = %+v err=%v", page2, err)
	}
	oldest, err := env.svc.ListQueue(context.Background(), QueueQuery{Limit: 10, Order: QueueOldest})
	if err != nil || len(oldest.Reports) != 2 || oldest.Reports[0].ID != first.ID || oldest.Reports[1].ID != second.ID {
		t.Fatalf("oldest = %+v err=%v", oldest, err)
	}
	st := StatusSubmitted
	tt := TargetListing
	rc := ReasonSpam
	filtered, err := env.svc.ListQueue(context.Background(), QueueQuery{Status: &st, TargetType: &tt, ReasonCode: &rc})
	if err != nil || len(filtered.Reports) != 1 || filtered.Reports[0].ID != first.ID {
		t.Fatalf("filtered = %+v err=%v", filtered, err)
	}
	if _, err := env.svc.ListQueue(context.Background(), QueueQuery{Limit: MaxQueueLimit + 1}); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("oversize limit err = %v", err)
	}
	bad := Status("open")
	if _, err := env.svc.ListQueue(context.Background(), QueueQuery{Status: &bad}); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("bad status filter err = %v", err)
	}
}

func TestValidAndInvalidStatusTransitions(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	row, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: listing, ReasonCode: ReasonSpam,
	})
	if err != nil {
		t.Fatal(err)
	}
	createdAt := row.CreatedAt
	env.now = env.now.Add(time.Second)
	actor := mustID(t)
	triaged, err := env.svc.Transition(context.Background(), row.ID, TransitionInput{
		To: StatusTriaged, StaffNote: " triage ", ActorID: &actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if triaged.Status != StatusTriaged || triaged.UpdatedAt.Equal(createdAt) {
		t.Fatalf("triaged = %+v", triaged)
	}
	if triaged.StaffNote == nil || *triaged.StaffNote != "triage" {
		t.Fatalf("note = %v", triaged.StaffNote)
	}
	if triaged.StatusChangedBy == nil || *triaged.StatusChangedBy != actor {
		t.Fatalf("actor = %v", triaged.StatusChangedBy)
	}
	if triaged.ReporterUserID != row.ReporterUserID || triaged.TargetID != row.TargetID || triaged.ReasonCode != row.ReasonCode {
		t.Fatalf("immutable mutated: %+v", triaged)
	}
	if _, err := env.svc.Transition(context.Background(), row.ID, TransitionInput{To: StatusSubmitted}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("jump back err = %v", err)
	}
	if _, err := env.svc.Transition(context.Background(), row.ID, TransitionInput{To: StatusClosed}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Transition(context.Background(), row.ID, TransitionInput{To: StatusTriaged}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
	fresh, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetListing, TargetID: listing, ReasonCode: ReasonOther,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Transition(context.Background(), fresh.ID, TransitionInput{To: StatusClosed}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("submitted to closed err = %v", err)
	}
	got, err := env.svc.GetReport(context.Background(), fresh.ID)
	if err != nil || got.Status != StatusSubmitted {
		t.Fatalf("fresh = %+v err=%v", got, err)
	}
}

type serviceEnv struct {
	svc             *Service
	store           *MemoryStore
	listings        *stubListings
	profiles        *stubProfiles
	profilesEnforce *stubProfileEnforcer
	outbox          *memoryWarningEnqueuer
	reporter        ID
	other           ID
	now             time.Time
}

func newServiceEnv(t *testing.T) *serviceEnv {
	t.Helper()
	env := &serviceEnv{
		store:    NewMemoryStore(),
		listings: &stubListings{rows: map[listingcontracts.ID]listingcontracts.ListingRef{}},
		profiles: &stubProfiles{byPublic: map[identitycontracts.ID]identitycontracts.ID{}},
		reporter: mustID(t),
		other:    mustID(t),
		now:      time.Unix(1_700_000_000, 0).UTC(),
	}
	svc, err := NewService(env.store, env.listings, env.profiles, DefaultPolicy(), func() time.Time { return env.now })
	if err != nil {
		t.Fatal(err)
	}
	env.svc = svc
	env.outbox = &memoryWarningEnqueuer{keys: make(map[string]struct{})}
	env.svc.SetOutbox(env.outbox)
	env.profilesEnforce = &stubProfileEnforcer{}
	env.svc.SetProfileEnforcement(env.profilesEnforce)
	return env
}

func (e *serviceEnv) publishListing(t *testing.T, owner ID) ID {
	t.Helper()
	id := mustID(t)
	e.listings.rows[listingcontracts.ID(id)] = listingcontracts.ListingRef{
		ID:          listingcontracts.ID(id),
		OwnerUserID: listingcontracts.ID(owner),
		Status:      listingcontracts.StatusPublished,
	}
	return id
}

func (e *serviceEnv) putProfile(t *testing.T, owner ID) ID {
	t.Helper()
	id := mustID(t)
	e.profiles.byPublic[identitycontracts.ID(id)] = identitycontracts.ID(owner)
	return id
}

type stubListings struct {
	rows map[listingcontracts.ID]listingcontracts.ListingRef
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

type stubProfiles struct {
	byPublic map[identitycontracts.ID]identitycontracts.ID
}

func (s *stubProfiles) ResolveByPublicID(_ context.Context, publicProfileID identitycontracts.ID) (identitycontracts.PublicProfile, error) {
	if _, ok := s.byPublic[publicProfileID]; !ok {
		return identitycontracts.PublicProfile{}, identitycontracts.ErrNotFound
	}
	return identitycontracts.PublicProfile{PublicProfileID: publicProfileID, MemberSince: time.Unix(1, 0).UTC()}, nil
}

func (s *stubProfiles) ResolveByUserID(_ context.Context, userID identitycontracts.ID) (identitycontracts.PublicProfile, error) {
	for publicID, owner := range s.byPublic {
		if owner == userID {
			return identitycontracts.PublicProfile{PublicProfileID: publicID, MemberSince: time.Unix(1, 0).UTC()}, nil
		}
	}
	return identitycontracts.PublicProfile{}, identitycontracts.ErrNotFound
}

func (s *stubProfiles) ResolveUserIDByPublicID(_ context.Context, publicProfileID identitycontracts.ID) (identitycontracts.ID, error) {
	owner, ok := s.byPublic[publicProfileID]
	if !ok {
		return identitycontracts.ID{}, identitycontracts.ErrNotFound
	}
	return owner, nil
}

type memoryWarningEnqueuer struct {
	events []outbox.Event
	keys   map[string]struct{}
	err    error
}

func (e *memoryWarningEnqueuer) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	if e == nil {
		return outbox.Event{}, errUnavailable
	}
	if e.err != nil {
		return outbox.Event{}, e.err
	}
	if e.keys == nil {
		e.keys = make(map[string]struct{})
	}
	if in.IdempotencyKey != "" {
		if _, ok := e.keys[in.IdempotencyKey]; ok {
			return outbox.Event{}, outbox.ErrConflict
		}
		e.keys[in.IdempotencyKey] = struct{}{}
	}
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	ev := outbox.Event{
		ID:           id,
		EventType:    in.EventType,
		EventVersion: in.EventVersion,
		Payload:      in.Payload,
		CreatedAt:    time.Unix(1, 0).UTC(),
		AvailableAt:  time.Unix(1, 0).UTC(),
	}
	if in.IdempotencyKey != "" {
		k := in.IdempotencyKey
		ev.IdempotencyKey = &k
	}
	e.events = append(e.events, ev)
	return ev, nil
}
