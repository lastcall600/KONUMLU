package moderation

import (
	"context"
	"errors"
	"testing"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	listingcontracts "backend/internal/listings/contracts"
)

func TestAcceptRestrictAppealClearsListingModeration(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	listing := env.publishListing(t, env.other)
	action := mustExecutedListingAction(t, env, listing, ActionTypeRestrict)
	accepted := mustAcceptAppeal(t, env, action)
	if len(enforcer.clears) != 1 || enforcer.clears[0] != listingcontracts.ID(listing) {
		t.Fatalf("clears = %+v", enforcer.clears)
	}
	assertRestorationHistory(t, env, accepted, action)
}

func TestAcceptRemoveAppealClearsListingModeration(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	listing := env.publishListing(t, env.other)
	action := mustExecutedListingAction(t, env, listing, ActionTypeRemove)
	accepted := mustAcceptAppeal(t, env, action)
	if len(enforcer.clears) != 1 || enforcer.clears[0] != listingcontracts.ID(listing) {
		t.Fatalf("clears = %+v", enforcer.clears)
	}
	assertRestorationHistory(t, env, accepted, action)
}

func TestAcceptAppealOnArchivedListingStillClearsModerationOnly(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	listing := env.publishListing(t, env.other)
	action := mustExecutedListingAction(t, env, listing, ActionTypeRemove)
	env.listings.rows[listingcontracts.ID(listing)] = listingcontracts.ListingRef{
		ID:              listingcontracts.ID(listing),
		OwnerUserID:     listingcontracts.ID(env.other),
		Status:          listingcontracts.StatusArchived,
		ModerationState: listingcontracts.ModerationStateRemoved,
	}
	mustAcceptAppeal(t, env, action)
	if len(enforcer.clears) != 1 {
		t.Fatalf("clears = %+v", enforcer.clears)
	}
	ref := env.listings.rows[listingcontracts.ID(listing)]
	if ref.Status != listingcontracts.StatusArchived {
		t.Fatalf("owner status mutated = %+v", ref)
	}
	if listingcontracts.PubliclyVisible(ref.Status, listingcontracts.ModerationStateNone) {
		t.Fatal("archived listing must stay non-public after restore")
	}
}

func TestFailedListingRestoreDoesNotAcceptAppeal(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	listing := env.publishListing(t, env.other)
	action := mustExecutedListingAction(t, env, listing, ActionTypeRestrict)
	row := mustAppealUnderReview(t, env, action)
	enforcer.clearErr = listingcontracts.ErrUnavailable
	if _, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusAccepted}); !errors.Is(err, errUnavailable) {
		t.Fatalf("failed restore err = %v", err)
	}
	got, err := env.svc.GetAppeal(context.Background(), row.CaseID, row.ID)
	if err != nil || got.Status != AppealStatusUnderReview {
		t.Fatalf("appeal = %+v err=%v", got, err)
	}
	hist, err := env.svc.GetCase(context.Background(), row.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hist.History {
		if h.Kind == HistoryAppealAccepted || h.Kind == HistoryAppealRestorationApplied {
			t.Fatalf("unexpected history = %+v", h)
		}
	}
	enforcer.clearErr = nil
	env.now = env.now.Add(time.Second)
	accepted, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusAccepted})
	if err != nil || accepted.Status != AppealStatusAccepted {
		t.Fatalf("retry accept = %+v err=%v", accepted, err)
	}
	if len(enforcer.clears) != 2 {
		t.Fatalf("retry clears = %+v", enforcer.clears)
	}
}

func TestRepeatedListingClearIsIdempotentOnAcceptPath(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	listing := env.publishListing(t, env.other)
	action := mustExecutedListingAction(t, env, listing, ActionTypeRemove)
	mustAcceptAppeal(t, env, action)
	if err := enforcer.ClearModerationState(context.Background(), listingcontracts.ID(listing)); err != nil {
		t.Fatal(err)
	}
	if len(enforcer.clears) != 2 {
		t.Fatalf("idempotent clears = %+v", enforcer.clears)
	}
}

func TestAcceptWarningAndNoActionDoesNotRestoreListing(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	listing := env.publishListing(t, env.other)
	for _, actionType := range []ActionType{ActionTypeNoAction, ActionTypeWarning} {
		action := mustExecutedListingAction(t, env, listing, actionType)
		mustAcceptAppeal(t, env, action)
	}
	if len(enforcer.clears) != 0 {
		t.Fatalf("unexpected clears = %+v", enforcer.clears)
	}
}

func TestAcceptPublicProfileAppealDoesNotRestoreListing(t *testing.T) {
	env := newServiceEnv(t)
	enforcer := &stubListingEnforcer{}
	env.svc.SetListingEnforcement(enforcer)
	profile := env.putProfile(t, env.other)
	action := mustExecutedAction(t, env, TargetPublicProfile, profile)
	mustAcceptAppeal(t, env, action)
	if len(enforcer.clears) != 0 || len(enforcer.calls) != 0 {
		t.Fatalf("profile restore calls = %+v clears=%+v", enforcer.calls, enforcer.clears)
	}
	if len(env.profilesEnforce.clears) != 0 {
		t.Fatalf("warning must not clear identity = %+v", env.profilesEnforce.clears)
	}
}

func TestAcceptPublicProfileRestrictAppealClearsIdentity(t *testing.T) {
	env := newServiceEnv(t)
	profile := env.putProfile(t, env.other)
	action := mustExecutedProfileAction(t, env, profile, ActionTypeRestrict)
	accepted := mustAcceptAppeal(t, env, action)
	if len(env.profilesEnforce.clears) != 1 || env.profilesEnforce.clears[0] != identitycontracts.ID(profile) {
		t.Fatalf("clears = %+v", env.profilesEnforce.clears)
	}
	assertRestorationHistory(t, env, accepted, action)
}

func TestAcceptPublicProfileRemoveAppealClearsIdentity(t *testing.T) {
	env := newServiceEnv(t)
	profile := env.putProfile(t, env.other)
	action := mustExecutedProfileAction(t, env, profile, ActionTypeRemove)
	accepted := mustAcceptAppeal(t, env, action)
	if len(env.profilesEnforce.clears) != 1 || env.profilesEnforce.clears[0] != identitycontracts.ID(profile) {
		t.Fatalf("clears = %+v", env.profilesEnforce.clears)
	}
	assertRestorationHistory(t, env, accepted, action)
}

func TestFailedIdentityRestoreDoesNotAcceptAppeal(t *testing.T) {
	env := newServiceEnv(t)
	profile := env.putProfile(t, env.other)
	action := mustExecutedProfileAction(t, env, profile, ActionTypeRestrict)
	row := mustAppealUnderReview(t, env, action)
	env.profilesEnforce.clearErr = identitycontracts.ErrUnavailable
	if _, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusAccepted}); !errors.Is(err, errUnavailable) {
		t.Fatalf("failed restore err = %v", err)
	}
	got, err := env.svc.GetAppeal(context.Background(), row.CaseID, row.ID)
	if err != nil || got.Status != AppealStatusUnderReview {
		t.Fatalf("appeal = %+v err=%v", got, err)
	}
	hist, err := env.svc.GetCase(context.Background(), row.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hist.History {
		if h.Kind == HistoryAppealAccepted || h.Kind == HistoryAppealRestorationApplied {
			t.Fatalf("unexpected history = %+v", h)
		}
	}
	env.profilesEnforce.clearErr = nil
	env.now = env.now.Add(time.Second)
	accepted, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusAccepted})
	if err != nil || accepted.Status != AppealStatusAccepted {
		t.Fatalf("retry accept = %+v err=%v", accepted, err)
	}
	if len(env.profilesEnforce.clears) != 2 {
		t.Fatalf("retry clears = %+v", env.profilesEnforce.clears)
	}
}

func TestRepeatedIdentityClearIsIdempotentOnAcceptPath(t *testing.T) {
	env := newServiceEnv(t)
	profile := env.putProfile(t, env.other)
	action := mustExecutedProfileAction(t, env, profile, ActionTypeRemove)
	mustAcceptAppeal(t, env, action)
	if err := env.profilesEnforce.ClearModerationState(context.Background(), identitycontracts.ID(profile)); err != nil {
		t.Fatal(err)
	}
	if len(env.profilesEnforce.clears) != 2 {
		t.Fatalf("idempotent clears = %+v", env.profilesEnforce.clears)
	}
}

func mustExecutedProfileAction(t *testing.T, env *serviceEnv, profile ID, actionType ActionType) CaseAction {
	t.Helper()
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetPublicProfile, SubjectID: profile, Title: "profile restore case",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetPublicProfile, TargetID: profile, ActionType: actionType,
		ReasonCode: ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	executed, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	return executed
}

func mustExecutedListingAction(t *testing.T, env *serviceEnv, listing ID, actionType ActionType) CaseAction {
	t.Helper()
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: listing, Title: "listing restore case",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: listing, ActionType: actionType,
		ReasonCode: ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	if _, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	executed, err := env.svc.TransitionAction(context.Background(), detail.Case.ID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	return executed
}

func mustAppealUnderReview(t *testing.T, env *serviceEnv, action CaseAction) Appeal {
	t.Helper()
	row, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: "please restore",
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	reviewing, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusUnderReview})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	return reviewing
}

func mustAcceptAppeal(t *testing.T, env *serviceEnv, action CaseAction) Appeal {
	t.Helper()
	row := mustAppealUnderReview(t, env, action)
	accepted, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusAccepted})
	if err != nil || accepted.Status != AppealStatusAccepted {
		t.Fatalf("accept = %+v err=%v", accepted, err)
	}
	return accepted
}

func assertRestorationHistory(t *testing.T, env *serviceEnv, appeal Appeal, action CaseAction) {
	t.Helper()
	got, err := env.svc.GetCase(context.Background(), appeal.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	var restored, accepted bool
	for _, h := range got.History {
		if h.Kind == HistoryAppealRestorationApplied {
			restored = true
			if h.AppealID == nil || *h.AppealID != appeal.ID {
				t.Fatalf("restore appeal id = %+v", h)
			}
			if h.ActionID == nil || *h.ActionID != action.ID {
				t.Fatalf("restore action id = %+v", h)
			}
		}
		if h.Kind == HistoryAppealAccepted {
			accepted = true
			if h.AppealID == nil || *h.AppealID != appeal.ID || h.ActionID != nil {
				t.Fatalf("accepted hist = %+v", h)
			}
		}
	}
	if !restored || !accepted {
		t.Fatalf("missing restoration/accept history: %+v", got.History)
	}
}
