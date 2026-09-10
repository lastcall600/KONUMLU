package moderation

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseIDRejectsZeroAndGarbage(t *testing.T) {
	if _, err := ParseID(""); !errors.Is(err, errZeroID) {
		t.Fatalf("empty err = %v", err)
	}
	if _, err := ParseID("not-a-uuid"); !errors.Is(err, errZeroID) {
		t.Fatalf("garbage err = %v", err)
	}
	if _, err := ParseID("00000000-0000-0000-0000-000000000000"); !errors.Is(err, errZeroID) {
		t.Fatalf("zero err = %v", err)
	}
}

func TestParseTargetAndReasonRejectArbitraryValues(t *testing.T) {
	if _, err := ParseTargetType("message"); !errors.Is(err, errInvalidTarget) {
		t.Fatalf("target err = %v", err)
	}
	if _, err := ParseReasonCode("custom"); !errors.Is(err, errInvalidReason) {
		t.Fatalf("reason err = %v", err)
	}
	if _, err := ParseReasonCode("spam"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseTargetType("public_profile"); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeDescriptionOptionalAndOversize(t *testing.T) {
	got, err := NormalizeDescription("   ")
	if err != nil || got != nil {
		t.Fatalf("empty got = %v err=%v", got, err)
	}
	got, err = NormalizeDescription("  ok  ")
	if err != nil || got == nil || *got != "ok" {
		t.Fatalf("trim got = %v err=%v", got, err)
	}
	if _, err := NormalizeDescription(strings.Repeat("a", MaxDescriptionBytes+1)); !errors.Is(err, errInvalidBody) {
		t.Fatalf("oversize err = %v", err)
	}
}

func TestCanTransitionOnlyAdjacentStatuses(t *testing.T) {
	if !CanTransition(StatusSubmitted, StatusTriaged) || !CanTransition(StatusTriaged, StatusClosed) {
		t.Fatal("expected allowed adjacent transitions")
	}
	if CanTransition(StatusSubmitted, StatusClosed) || CanTransition(StatusClosed, StatusTriaged) ||
		CanTransition(StatusTriaged, StatusSubmitted) || CanTransition(StatusSubmitted, StatusSubmitted) {
		t.Fatal("unexpected jump allowed")
	}
}

func TestCanTransitionCasePipelineOnly(t *testing.T) {
	if !CanTransitionCase(CaseStatusOpen, CaseStatusInvestigating) ||
		!CanTransitionCase(CaseStatusInvestigating, CaseStatusResolved) ||
		!CanTransitionCase(CaseStatusResolved, CaseStatusClosed) {
		t.Fatal("expected allowed case pipeline")
	}
	if CanTransitionCase(CaseStatusOpen, CaseStatusClosed) || CanTransitionCase(CaseStatusClosed, CaseStatusOpen) ||
		CanTransitionCase(CaseStatusInvestigating, CaseStatusOpen) {
		t.Fatal("unexpected case jump allowed")
	}
}

func TestNormalizeStaffNoteOptionalAndOversize(t *testing.T) {
	got, err := NormalizeStaffNote("  ")
	if err != nil || got != nil {
		t.Fatalf("empty got = %v err=%v", got, err)
	}
	if _, err := NormalizeStaffNote(strings.Repeat("a", MaxStaffNoteBytes+1)); !errors.Is(err, errInvalidBody) {
		t.Fatalf("oversize err = %v", err)
	}
}

func TestReportValidateRequiresKnownCodes(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	row := Report{
		ID: mustID(t), ReporterUserID: mustID(t), TargetType: TargetListing,
		TargetID: mustID(t), ReasonCode: ReasonSpam, Status: StatusSubmitted,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := row.Validate(); err != nil {
		t.Fatal(err)
	}
	row.ReasonCode = "made_up"
	if err := row.Validate(); !errors.Is(err, errInvalidReason) {
		t.Fatalf("reason err = %v", err)
	}
}

func TestParseEvidenceTypeRejectsUnknown(t *testing.T) {
	if _, err := ParseEvidenceType("media_blob"); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("unknown err = %v", err)
	}
	if _, err := ParseEvidenceType("staff_note"); err != nil {
		t.Fatal(err)
	}
}

func TestParseActionTypeStatusAndReason(t *testing.T) {
	if _, err := ParseActionType("shadowban"); !errors.Is(err, errInvalidAction) {
		t.Fatalf("type err = %v", err)
	}
	if _, err := ParseActionType("remove"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseActionStatus("pending"); !errors.Is(err, errInvalidActionStatus) {
		t.Fatalf("status err = %v", err)
	}
	if _, err := ParseActionReasonCode("spam"); !errors.Is(err, errInvalidAction) {
		t.Fatalf("report reason as action reason err = %v", err)
	}
}

func TestCanTransitionActionLifecycleOnly(t *testing.T) {
	if !CanTransitionAction(ActionStatusProposed, ActionStatusApproved) ||
		!CanTransitionAction(ActionStatusProposed, ActionStatusCancelled) ||
		!CanTransitionAction(ActionStatusApproved, ActionStatusExecuted) ||
		!CanTransitionAction(ActionStatusApproved, ActionStatusCancelled) {
		t.Fatal("expected allowed action pipeline")
	}
	if CanTransitionAction(ActionStatusProposed, ActionStatusExecuted) ||
		CanTransitionAction(ActionStatusExecuted, ActionStatusCancelled) ||
		CanTransitionAction(ActionStatusCancelled, ActionStatusProposed) ||
		CanTransitionAction(ActionStatusExecuted, ActionStatusApproved) {
		t.Fatal("unexpected action jump allowed")
	}
}

func TestCanTransitionAppealLifecycleOnly(t *testing.T) {
	if !CanTransitionAppeal(AppealStatusSubmitted, AppealStatusUnderReview) ||
		!CanTransitionAppeal(AppealStatusSubmitted, AppealStatusWithdrawn) ||
		!CanTransitionAppeal(AppealStatusUnderReview, AppealStatusAccepted) ||
		!CanTransitionAppeal(AppealStatusUnderReview, AppealStatusRejected) {
		t.Fatal("expected allowed appeal pipeline")
	}
	if CanTransitionAppeal(AppealStatusSubmitted, AppealStatusAccepted) ||
		CanTransitionAppeal(AppealStatusUnderReview, AppealStatusWithdrawn) ||
		CanTransitionAppeal(AppealStatusAccepted, AppealStatusUnderReview) ||
		CanTransitionAppeal(AppealStatusRejected, AppealStatusSubmitted) ||
		CanStaffTransitionAppeal(AppealStatusSubmitted, AppealStatusWithdrawn) {
		t.Fatal("unexpected appeal jump allowed")
	}
}

func TestAppealRestorationHistoryRequiresActionAndAppeal(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	appealID := mustID(t)
	actionID := mustID(t)
	row := CaseHistory{
		ID: mustID(t), CaseID: mustID(t), Kind: HistoryAppealRestorationApplied,
		AppealID: &appealID, ActionID: &actionID, CreatedAt: now,
	}
	if err := row.Validate(); err != nil {
		t.Fatal(err)
	}
	row.ActionID = nil
	if err := row.Validate(); !errors.Is(err, errInvalidHistory) {
		t.Fatalf("missing action err = %v", err)
	}
	row.ActionID = &actionID
	row.AppealID = nil
	if err := row.Validate(); !errors.Is(err, errInvalidHistory) {
		t.Fatalf("missing appeal err = %v", err)
	}
	if _, err := ParseCaseHistoryKind("appeal_restoration_applied"); err != nil {
		t.Fatal(err)
	}
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
