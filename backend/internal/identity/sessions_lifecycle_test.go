package identity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestListActiveForUserOmitsRevokedAndForeign(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestSessions(t)
	user := store.putUser(t, User{})
	other := store.putUser(t, User{})
	a, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, other.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, b.Session.ID); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListActiveForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != a.Session.ID {
		t.Fatalf("list = %+v", list)
	}
	if list[0].TokenHash != nil {
		t.Fatal("list must not return token hash")
	}
}

func TestRevokeOthersKeepsCurrentAndDoesNotBumpEpoch(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestSessions(t)
	user := store.putUser(t, User{})
	keep, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeOthers(ctx, user.ID, keep.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, keep.RawToken); err != nil {
		t.Fatalf("current session: %v", err)
	}
	if _, err := svc.Resolve(ctx, other.RawToken); err != errSessionRevoked {
		t.Fatalf("other session: %v", err)
	}
	if store.users[user.ID].SessionEpoch != 0 {
		t.Fatalf("epoch = %d, want 0", store.users[user.ID].SessionEpoch)
	}
}

func TestGetOwnedHidesForeignSession(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestSessions(t)
	a := store.putUser(t, User{})
	b := store.putUser(t, User{})
	issued, err := svc.Create(ctx, b.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetOwned(ctx, a.ID, issued.Session.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
}

func TestResolveDoesNotTouchWithinQuantum(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{at: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)}
	store := newMemStore()
	svc := mustSessions(t, store, clock.now)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	createdSeen := store.mustSession(t, issued.Session.ID).LastSeenAt
	clock.at = clock.at.Add(30 * time.Second)
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	if store.mustSession(t, issued.Session.ID).LastSeenAt != createdSeen {
		t.Fatal("touch must be throttled inside quantum")
	}
	clock.at = clock.at.Add(time.Minute)
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	if store.mustSession(t, issued.Session.ID).LastSeenAt != clock.at {
		t.Fatal("touch must persist after quantum")
	}
}

func TestTouchDoesNotReviveRevokedSession(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestSessions(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, issued.Session.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Touch(ctx, issued.Session.ID); !errors.Is(err, errSessionRevoked) {
		t.Fatalf("touch err = %v", err)
	}
	got := store.mustSession(t, issued.Session.ID)
	if got.RevokedAt == nil || got.LastSeenAt != now() {
		t.Fatalf("revoked session mutated: %+v", got)
	}
}

func TestConcurrentRevokeAndTouch(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestSessions(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = svc.Revoke(ctx, issued.Session.ID)
	}()
	go func() {
		defer wg.Done()
		_ = svc.Touch(ctx, issued.Session.ID)
	}()
	wg.Wait()
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errSessionRevoked {
		t.Fatalf("after concurrent revoke/touch: %v", err)
	}
	if store.mustSession(t, issued.Session.ID).RevokedAt == nil {
		t.Fatal("session must stay revoked")
	}
}
