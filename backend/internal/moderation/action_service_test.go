package moderation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreateActionMatchesCaseSubject(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	actor := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "actions",
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonPolicyViolation, Rationale: "  internal note  ", ActorID: &actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != ActionStatusProposed || row.ActionType != ActionTypeWarning {
		t.Fatalf("action = %+v", row)
	}
	if row.Rationale == nil || *row.Rationale != "internal note" {
		t.Fatalf("rationale = %+v", row.Rationale)
	}
	if row.ActorStaffID == nil || *row.ActorStaffID != actor {
		t.Fatalf("actor = %+v", row.ActorStaffID)
	}
	got, err := env.svc.GetCase(context.Background(), detail.Case.ID)
	if err != nil || len(got.History) != 2 {
		t.Fatalf("history = %+v err=%v", got.History, err)
	}
	if got.History[1].Kind != HistoryActionProposed || got.History[1].ActionID == nil || *got.History[1].ActionID != row.ID {
		t.Fatalf("hist = %+v", got.History[1])
	}
}

func TestCreateActionRejectsTargetMismatch(t *testing.T) {
	env := newServiceEnv(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: mustID(t), Title: "mismatch",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetPublicProfile, TargetID: mustID(t), ActionType: ActionTypeRemove,
		ReasonCode: ActionReasonSafetyRisk,
	}); !errors.Is(err, errInvalidAction) {
		t.Fatalf("type mismatch err = %v", err)
	}
	if _, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: mustID(t), ActionType: ActionTypeRemove,
		ReasonCode: ActionReasonSafetyRisk,
	}); !errors.Is(err, errInvalidAction) {
		t.Fatalf("id mismatch err = %v", err)
	}
}

func TestCreateActionRejectedOnClosedCase(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "closed",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Case.ID
	mustCloseCase(t, env, id)
	if _, err := env.svc.CreateAction(context.Background(), id, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeNoAction,
		ReasonCode: ActionReasonNoViolation,
	}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("closed create err = %v", err)
	}
}

func TestActionLifecycleAndInvalidJumps(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetPublicProfile, SubjectID: subject, Title: "lifecycle",
	})
	if err != nil {
		t.Fatal(err)
	}
	caseID := detail.Case.ID
	row, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetPublicProfile, TargetID: subject, ActionType: ActionTypeRestrict,
		ReasonCode: ActionReasonRepeatedViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusExecuted}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("proposed->executed err = %v", err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusProposed}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("same-status err = %v", err)
	}
	env.now = env.now.Add(time.Second)
	approved, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusApproved})
	if err != nil || approved.Status != ActionStatusApproved {
		t.Fatalf("approve = %+v err=%v", approved, err)
	}
	if approved.ActionType != ActionTypeRestrict || approved.TargetID != subject {
		t.Fatalf("immutable mutated = %+v", approved)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusProposed}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
	env.now = env.now.Add(time.Second)
	executed, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusExecuted})
	if err != nil || executed.Status != ActionStatusExecuted {
		t.Fatalf("execute = %+v err=%v", executed, err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusCancelled}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("executed terminal cancel err = %v", err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, row.ID, ActionTransitionInput{To: ActionStatusApproved}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("executed reopen err = %v", err)
	}
	got, err := env.svc.GetCase(context.Background(), caseID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]CaseHistoryKind, 0, len(got.History))
	for _, h := range got.History {
		if h.ActionID != nil {
			kinds = append(kinds, h.Kind)
			if *h.ActionID != row.ID {
				t.Fatalf("history action id = %+v", h)
			}
		}
	}
	if len(kinds) != 3 || kinds[0] != HistoryActionProposed || kinds[1] != HistoryActionApproved || kinds[2] != HistoryActionExecuted {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestActionCancelFromProposedAndApproved(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "cancel",
	})
	if err != nil {
		t.Fatal(err)
	}
	caseID := detail.Case.ID
	first, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonOther,
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := env.svc.TransitionAction(context.Background(), caseID, first.ID, ActionTransitionInput{To: ActionStatusCancelled})
	if err != nil || cancelled.Status != ActionStatusCancelled {
		t.Fatalf("cancel proposed = %+v err=%v", cancelled, err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, first.ID, ActionTransitionInput{To: ActionStatusApproved}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("cancelled reopen err = %v", err)
	}
	env.now = env.now.Add(time.Second)
	second, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeSuspend,
		ReasonCode: ActionReasonSafetyRisk,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, second.ID, ActionTransitionInput{To: ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, second.ID, ActionTransitionInput{To: ActionStatusCancelled}); err != nil {
		t.Fatal(err)
	}
}

func TestActionHistoryAtomicWithStoreFailure(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "atomic",
	})
	if err != nil {
		t.Fatal(err)
	}
	caseID := detail.Case.ID
	first, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeNoAction,
		ReasonCode: ActionReasonNoViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.store.SetFail(errUnavailable)
	if _, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeRemove,
		ReasonCode: ActionReasonProhibitedContent,
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("fail create err = %v", err)
	}
	if _, err := env.svc.TransitionAction(context.Background(), caseID, first.ID, ActionTransitionInput{To: ActionStatusApproved}); !errors.Is(err, errUnavailable) {
		t.Fatalf("fail transition err = %v", err)
	}
	env.store.SetFail(nil)
	rows, err := env.svc.ListActions(context.Background(), caseID, 10)
	if err != nil || len(rows) != 1 || rows[0].ID != first.ID || rows[0].Status != ActionStatusProposed {
		t.Fatalf("partial actions = %+v err=%v", rows, err)
	}
	hist, err := env.svc.GetCase(context.Background(), caseID)
	if err != nil || len(hist.History) != 2 {
		t.Fatalf("partial history = %+v err=%v", hist.History, err)
	}
}

func TestActionListDeterministicOrder(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "order",
	})
	if err != nil {
		t.Fatal(err)
	}
	caseID := detail.Case.ID
	same := env.now
	first, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := env.svc.CreateAction(context.Background(), caseID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeRestrict,
		ReasonCode: ActionReasonRepeatedViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.CreatedAt.Equal(same) || !second.CreatedAt.Equal(same) {
		t.Fatalf("expected same created_at first=%v second=%v", first.CreatedAt, second.CreatedAt)
	}
	rows, err := env.svc.ListActions(context.Background(), caseID, 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v err=%v", rows, err)
	}
	if bytesCmp(rows[0].ID, rows[1].ID) >= 0 {
		t.Fatalf("id order = %s then %s", rows[0].ID, rows[1].ID)
	}
}

func TestCreateActionValidation(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "validate",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Case.ID
	if _, err := env.svc.CreateAction(context.Background(), id, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: "ban_forever",
		ReasonCode: ActionReasonOther,
	}); !errors.Is(err, errInvalidAction) {
		t.Fatalf("type err = %v", err)
	}
	if _, err := env.svc.CreateAction(context.Background(), id, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeWarning,
		ReasonCode: "custom",
	}); !errors.Is(err, errInvalidAction) {
		t.Fatalf("reason err = %v", err)
	}
	if _, err := env.svc.CreateAction(context.Background(), id, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonOther, Rationale: strings.Repeat("a", MaxActionRationaleBytes+1),
	}); !errors.Is(err, errInvalidBody) {
		t.Fatalf("rationale oversize err = %v", err)
	}
	if _, err := env.svc.CreateAction(context.Background(), id, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonOther, Rationale: "Authorization: Bearer secret",
	}); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("secret rationale err = %v", err)
	}
	if _, err := env.svc.CreateAction(context.Background(), mustID(t), CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeWarning,
		ReasonCode: ActionReasonOther,
	}); !errors.Is(err, errNotFound) {
		t.Fatalf("missing case err = %v", err)
	}
	if _, err := env.svc.GetAction(context.Background(), id, mustID(t)); !errors.Is(err, errNotFound) {
		t.Fatalf("missing action err = %v", err)
	}
}

func TestActionDoesNotInventStaffIdentity(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "no actor",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.CreateAction(context.Background(), detail.Case.ID, CreateActionInput{
		TargetType: TargetListing, TargetID: subject, ActionType: ActionTypeNoAction,
		ReasonCode: ActionReasonNoViolation,
	})
	if err != nil || row.ActorStaffID != nil {
		t.Fatalf("invented actor = %+v err=%v", row, err)
	}
}

func mustCloseCase(t *testing.T, env *serviceEnv, id ID) {
	t.Helper()
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusInvestigating}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusResolved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusClosed}); err != nil {
		t.Fatal(err)
	}
}
