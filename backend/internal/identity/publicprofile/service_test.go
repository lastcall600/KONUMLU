package publicprofile

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/identity"
	"backend/internal/identity/contracts"
)

func TestGetMeLazyZeroState(t *testing.T) {
	svc, store, user := newTestService(t)
	view, err := svc.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.PublicProfileID.IsZero() || view.PublicProfileID == user.ID {
		t.Fatalf("public id = %s user = %s", view.PublicProfileID, user.ID)
	}
	if view.DisplayName != nil {
		t.Fatalf("display = %v", view.DisplayName)
	}
	if !view.MemberSince.Equal(user.CreatedAt.UTC()) {
		t.Fatalf("memberSince = %s want %s", view.MemberSince, user.CreatedAt)
	}
	again, err := svc.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.PublicProfileID != view.PublicProfileID {
		t.Fatal("public id must be stable")
	}
	if _, err := store.GetByUserID(context.Background(), user.ID); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateDisplayName(t *testing.T) {
	svc, _, user := newTestService(t)
	name := "  Konumlu Ada  "
	view, err := svc.UpdateMe(context.Background(), user.ID, &name)
	if err != nil {
		t.Fatal(err)
	}
	if view.DisplayName == nil || *view.DisplayName != "Konumlu Ada" {
		t.Fatalf("display = %v", view.DisplayName)
	}
	html := "<b>x</b>"
	if _, err := svc.UpdateMe(context.Background(), user.ID, &html); !errors.Is(err, ErrInvalidDisplayName) {
		t.Fatalf("html err = %v", err)
	}
}

func TestGetPublicLookup(t *testing.T) {
	svc, _, user := newTestService(t)
	me, err := svc.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := svc.GetPublic(context.Background(), me.PublicProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if pub.PublicProfileID != me.PublicProfileID || pub.DisplayName != nil {
		t.Fatalf("pub = %+v", pub)
	}
}

func TestPublicOmitsInternalUserID(t *testing.T) {
	svc, _, user := newTestService(t)
	me, err := svc.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(toDTO(me))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ToLower(string(raw))
	if strings.Contains(body, "userid") || strings.Contains(body, "user_id") || strings.Contains(body, user.ID.String()) {
		t.Fatalf("leaked user id: %s", raw)
	}
	if strings.Contains(body, "email") || strings.Contains(body, "phone") {
		t.Fatalf("leaked identifier field: %s", raw)
	}
}

func TestDisabledUserNotPublic(t *testing.T) {
	svc, store, user := newTestService(t)
	me, err := svc.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
	user.DisabledAt = &at
	store.PutUser(user)
	if _, err := svc.GetPublic(context.Background(), me.PublicProfileID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled err = %v", err)
	}
	user.DisabledAt = nil
	user.DeletedAt = &at
	store.PutUser(user)
	if _, err := svc.GetPublic(context.Background(), me.PublicProfileID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted err = %v", err)
	}
}

func TestMissingProfileIsNotFound(t *testing.T) {
	svc, _, _ := newTestService(t)
	missing := mustID(t)
	if _, err := svc.GetPublic(context.Background(), missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestInvalidPublicID(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.GetPublic(context.Background(), identity.ID{}); !errors.Is(err, ErrInvalidPublicID) {
		t.Fatalf("err = %v", err)
	}
}

func TestRestrictRemoveHidePublicAndClearRestores(t *testing.T) {
	svc, store, user := newTestService(t)
	me, err := svc.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublic(context.Background(), me.PublicProfileID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyPublicProfileModerationInput{
		PublicProfileID: toContractID(me.PublicProfileID), State: contracts.ModerationStateRestricted,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublic(context.Background(), me.PublicProfileID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restricted public err = %v", err)
	}
	self, err := svc.GetMe(context.Background(), user.ID)
	if err != nil || self.PublicProfileID != me.PublicProfileID {
		t.Fatalf("self after restrict = %+v err=%v", self, err)
	}
	got, err := store.GetByUserID(context.Background(), user.ID)
	if err != nil || got.ModerationState != ModerationRestricted {
		t.Fatalf("stored restrict = %+v err=%v", got, err)
	}
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyPublicProfileModerationInput{
		PublicProfileID: toContractID(me.PublicProfileID), State: contracts.ModerationStateRestricted,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyPublicProfileModerationInput{
		PublicProfileID: toContractID(me.PublicProfileID), State: contracts.ModerationStateRemoved,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublic(context.Background(), me.PublicProfileID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed public err = %v", err)
	}
	if err := svc.ClearModerationState(context.Background(), toContractID(me.PublicProfileID)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublic(context.Background(), me.PublicProfileID); err != nil {
		t.Fatalf("restored public err = %v", err)
	}
	if err := svc.ClearModerationState(context.Background(), toContractID(me.PublicProfileID)); err != nil {
		t.Fatal(err)
	}
}

func TestNoActionEquivalentDoesNotHide(t *testing.T) {
	svc, _, user := newTestService(t)
	me, err := svc.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyPublicProfileModerationInput{
		PublicProfileID: toContractID(me.PublicProfileID), State: contracts.ModerationStateNone,
	}); !errors.Is(err, contracts.ErrModerationNotEnforced) {
		t.Fatalf("none apply err = %v", err)
	}
	if _, err := svc.GetPublic(context.Background(), me.PublicProfileID); err != nil {
		t.Fatal(err)
	}
}

func TestRestrictDoesNotDisableAccountOrSessions(t *testing.T) {
	svc, store, user := newTestService(t)
	me, err := svc.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyPublicProfileModerationInput{
		PublicProfileID: toContractID(me.PublicProfileID), State: contracts.ModerationStateRemoved,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetUser(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisabledAt != nil || got.DeletedAt != nil || got.SessionEpoch != user.SessionEpoch {
		t.Fatalf("account mutated = %+v", got)
	}
	if !got.EligibleForSession() {
		t.Fatal("login eligibility changed")
	}
}

func TestFailedApplyLeavesPublic(t *testing.T) {
	svc, store, user := newTestService(t)
	me, err := svc.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	store.SetFail(ErrUnavailable)
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyPublicProfileModerationInput{
		PublicProfileID: toContractID(me.PublicProfileID), State: contracts.ModerationStateRestricted,
	}); !errors.Is(err, contracts.ErrUnavailable) {
		t.Fatalf("apply err = %v", err)
	}
	store.SetFail(nil)
	if _, err := svc.GetPublic(context.Background(), me.PublicProfileID); err != nil {
		t.Fatalf("still public err = %v", err)
	}
}

func TestOwnerMappingSurvivesHideForAppeals(t *testing.T) {
	svc, _, user := newTestService(t)
	res, err := NewResolver(svc)
	if err != nil {
		t.Fatal(err)
	}
	me, err := svc.GetMe(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyPublicProfileModerationInput{
		PublicProfileID: toContractID(me.PublicProfileID), State: contracts.ModerationStateRestricted,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := res.ResolveByPublicID(context.Background(), toContractID(me.PublicProfileID)); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("public resolve err = %v", err)
	}
	if _, err := res.ResolveByUserID(context.Background(), toContractID(user.ID)); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("seller resolve err = %v", err)
	}
	mapped, err := res.ResolveUserIDByPublicID(context.Background(), toContractID(me.PublicProfileID))
	if err != nil || mapped != toContractID(user.ID) {
		t.Fatalf("owner map = %s err=%v", mapped, err)
	}
}

func TestResolverDoesNotExposeUserID(t *testing.T) {
	svc, _, user := newTestService(t)
	res, err := NewResolver(svc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := res.ResolveByUserID(context.Background(), toContractID(user.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got.PublicProfileID.IsZero() {
		t.Fatal("missing public id")
	}
	pub, err := res.ResolveByPublicID(context.Background(), got.PublicProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if pub.PublicProfileID != got.PublicProfileID {
		t.Fatalf("mismatch %+v %+v", pub, got)
	}
	raw, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "userid") || strings.Contains(string(raw), user.ID.String()) {
		t.Fatalf("contract leaked user id: %s", raw)
	}
	mapped, err := res.ResolveUserIDByPublicID(context.Background(), got.PublicProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if mapped != toContractID(user.ID) {
		t.Fatalf("mapped user = %s want %s", mapped, user.ID)
	}
	var _ contracts.PublicProfileResolver = res
}

func newTestService(t *testing.T) (*Service, *MemoryStore, identity.User) {
	t.Helper()
	store := NewMemoryStore()
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	user := identity.User{ID: mustID(t), CreatedAt: now, UpdatedAt: now}
	store.PutUser(user)
	svc, err := NewService(store, func() time.Time { return now.Add(time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, user
}

func mustID(t *testing.T) identity.ID {
	t.Helper()
	id, err := identity.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
