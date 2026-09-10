package moderation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEligibleSubjectCanAppealExecutedAction(t *testing.T) {
	env := newServiceEnv(t)
	owner := env.other
	listing := env.publishListing(t, owner)
	action := mustExecutedAction(t, env, TargetListing, listing)
	row, err := env.svc.SubmitAppeal(context.Background(), owner, CreateAppealInput{
		ActionID: action.ID, Statement: "  this listing is legitimate  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != AppealStatusSubmitted || row.AppellantUserID != owner || row.Statement != "this listing is legitimate" {
		t.Fatalf("appeal = %+v", row)
	}
	if row.ActionID != action.ID || row.CaseID != action.CaseID {
		t.Fatalf("refs = %+v action=%+v", row, action)
	}
	got, err := env.svc.GetCase(context.Background(), action.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	last := got.History[len(got.History)-1]
	if last.Kind != HistoryAppealSubmitted || last.AppealID == nil || *last.AppealID != row.ID {
		t.Fatalf("hist = %+v", last)
	}
	if last.ActorStaffID != nil {
		t.Fatalf("consumer submit invented staff actor: %+v", last)
	}
}

func TestUnrelatedUserCannotDiscoverOrAppeal(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	action := mustExecutedAction(t, env, TargetListing, listing)
	if _, err := env.svc.SubmitAppeal(context.Background(), env.reporter, CreateAppealInput{
		ActionID: action.ID, Statement: "not mine",
	}); !errors.Is(err, errNotFound) {
		t.Fatalf("unrelated submit err = %v", err)
	}
	created, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: "owner appeal",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.GetAppealForUser(context.Background(), env.reporter, created.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("unrelated get err = %v", err)
	}
	if _, err := env.svc.WithdrawAppeal(context.Background(), env.reporter, created.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("unrelated withdraw err = %v", err)
	}
	mine, err := env.svc.ListMineAppeals(context.Background(), env.reporter)
	if err != nil || len(mine) != 0 {
		t.Fatalf("unrelated list = %+v err=%v", mine, err)
	}
}

func TestDuplicateActiveAppealRejected(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	action := mustExecutedAction(t, env, TargetListing, listing)
	if _, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: "first",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: "second",
	}); !errors.Is(err, errConflict) {
		t.Fatalf("duplicate err = %v", err)
	}
}

func TestAppealWindowEnforced(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	action := mustExecutedAction(t, env, TargetListing, listing)
	env.now = action.UpdatedAt.Add(DefaultAppealWindow + time.Second)
	if _, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: "too late",
	}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("window err = %v", err)
	}
}

func TestAppealLifecycleAndInvalidJumps(t *testing.T) {
	env := newServiceEnv(t)
	profile := env.putProfile(t, env.other)
	action := mustExecutedAction(t, env, TargetPublicProfile, profile)
	row, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: "please review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusAccepted}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("skip review err = %v", err)
	}
	if _, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusWithdrawn}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("staff withdraw err = %v", err)
	}
	staff := mustID(t)
	env.now = env.now.Add(time.Second)
	reviewing, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusUnderReview, ActorID: &staff})
	if err != nil || reviewing.Status != AppealStatusUnderReview {
		t.Fatalf("review = %+v err=%v", reviewing, err)
	}
	if _, err := env.svc.WithdrawAppeal(context.Background(), env.other, row.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("withdraw under review err = %v", err)
	}
	env.now = env.now.Add(time.Second)
	accepted, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusAccepted, ActorID: &staff})
	if err != nil || accepted.Status != AppealStatusAccepted {
		t.Fatalf("accept = %+v err=%v", accepted, err)
	}
	if accepted.DecidedByStaffID == nil || *accepted.DecidedByStaffID != staff || accepted.DecidedAt == nil {
		t.Fatalf("decision fields = %+v", accepted)
	}
	if _, err := env.svc.TransitionAppeal(context.Background(), row.CaseID, row.ID, AppealTransitionInput{To: AppealStatusRejected}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
	still, err := env.svc.GetAction(context.Background(), row.CaseID, action.ID)
	if err != nil || still.Status != ActionStatusExecuted {
		t.Fatalf("enforcement reversed = %+v err=%v", still, err)
	}
	got, err := env.svc.GetCase(context.Background(), row.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]CaseHistoryKind, 0)
	for _, h := range got.History {
		if h.AppealID != nil {
			kinds = append(kinds, h.Kind)
			if *h.AppealID != row.ID {
				t.Fatalf("history appeal id = %+v", h)
			}
		}
	}
	if len(kinds) != 3 || kinds[0] != HistoryAppealSubmitted || kinds[1] != HistoryAppealReviewStarted || kinds[2] != HistoryAppealAccepted {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestAppealWithdrawal(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	action := mustExecutedAction(t, env, TargetListing, listing)
	row, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: "withdraw me",
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	withdrawn, err := env.svc.WithdrawAppeal(context.Background(), env.other, row.ID)
	if err != nil || withdrawn.Status != AppealStatusWithdrawn || withdrawn.DecidedAt == nil || withdrawn.DecidedByStaffID != nil {
		t.Fatalf("withdraw = %+v err=%v", withdrawn, err)
	}
	if _, err := env.svc.WithdrawAppeal(context.Background(), env.other, row.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("terminal withdraw err = %v", err)
	}
	got, err := env.svc.GetCase(context.Background(), row.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	last := got.History[len(got.History)-1]
	if last.Kind != HistoryAppealWithdrawn || last.AppealID == nil || *last.AppealID != row.ID {
		t.Fatalf("hist = %+v", last)
	}
}

func TestAppealHistoryAtomicWithStoreFailure(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	action := mustExecutedAction(t, env, TargetListing, listing)
	first, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: "keep",
	})
	if err != nil {
		t.Fatal(err)
	}
	env.store.SetFail(errUnavailable)
	if _, err := env.svc.TransitionAppeal(context.Background(), first.CaseID, first.ID, AppealTransitionInput{To: AppealStatusUnderReview}); !errors.Is(err, errUnavailable) {
		t.Fatalf("fail transition err = %v", err)
	}
	env.store.SetFail(nil)
	got, err := env.svc.GetAppeal(context.Background(), first.CaseID, first.ID)
	if err != nil || got.Status != AppealStatusSubmitted {
		t.Fatalf("partial appeal = %+v err=%v", got, err)
	}
	hist, err := env.svc.GetCase(context.Background(), first.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	appealKinds := 0
	for _, h := range hist.History {
		if h.AppealID != nil {
			appealKinds++
		}
	}
	if appealKinds != 1 {
		t.Fatalf("partial history kinds = %d hist=%+v", appealKinds, hist.History)
	}
}

func TestSubmitAppealRejectsSpoofAndInvalid(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	action := mustExecutedAction(t, env, TargetListing, listing)
	if _, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: strings.Repeat("a", MaxAppealStatementBytes+1),
	}); !errors.Is(err, errInvalidBody) {
		t.Fatalf("oversize err = %v", err)
	}
	if _, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: mustID(t), Statement: "missing action",
	}); !errors.Is(err, errNotFound) {
		t.Fatalf("missing action err = %v", err)
	}
	proposed, err := env.svc.CreateAction(context.Background(), action.CaseID, CreateActionInput{
		TargetType: TargetListing, TargetID: listing, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: proposed.ID, Statement: "too early",
	}); !errors.Is(err, errNotFound) {
		t.Fatalf("proposed appeal err = %v", err)
	}
}

func TestNewAppealAllowedAfterTerminalIfStillInWindow(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	action := mustExecutedAction(t, env, TargetListing, listing)
	first, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.WithdrawAppeal(context.Background(), env.other, first.ID); err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	second, err := env.svc.SubmitAppeal(context.Background(), env.other, CreateAppealInput{
		ActionID: action.ID, Statement: "again",
	})
	if err != nil || second.Status != AppealStatusSubmitted {
		t.Fatalf("second = %+v err=%v", second, err)
	}
}

func mustExecutedAction(t *testing.T, env *serviceEnv, targetType TargetType, subject ID) CaseAction {
	t.Helper()
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: targetType, SubjectID: subject, Title: "appeal case",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: targetType, TargetID: subject, ActionType: ActionTypeWarning,
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
