package moderation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateCaseWithoutReports(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "  spam cluster  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Case.Status != CaseStatusOpen || detail.Case.Priority != CasePriorityNormal {
		t.Fatalf("case = %+v", detail.Case)
	}
	if detail.Case.Title != "spam cluster" || len(detail.ReportIDs) != 0 {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.History) != 1 || detail.History[0].Kind != HistoryCaseCreated {
		t.Fatalf("history = %+v", detail.History)
	}
	if detail.History[0].ActorStaffID != nil {
		t.Fatalf("invented actor: %+v", detail.History[0].ActorStaffID)
	}
}

func TestCreateCaseAttachesMultipleReports(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	first := env.mustReport(t, listing, ReasonSpam)
	env.now = env.now.Add(time.Second)
	second := env.mustReport(t, listing, ReasonScamOrFraud)
	actor := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: listing, Title: "listing review",
		ReportIDs: []ID{first.ID, second.ID}, ActorID: &actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.ReportIDs) != 2 {
		t.Fatalf("report ids = %v", detail.ReportIDs)
	}
	if len(detail.History) != 3 {
		t.Fatalf("history len = %d", len(detail.History))
	}
	if detail.History[0].Kind != HistoryCaseCreated || detail.History[1].Kind != HistoryReportAttached || detail.History[2].Kind != HistoryReportAttached {
		t.Fatalf("history kinds = %+v", detail.History)
	}
	if detail.History[0].ActorStaffID == nil || *detail.History[0].ActorStaffID != actor {
		t.Fatalf("actor = %+v", detail.History[0].ActorStaffID)
	}
	got, err := env.svc.GetReport(context.Background(), first.ID)
	if err != nil || got.Description != first.Description || got.Status != StatusSubmitted {
		t.Fatalf("report mutated = %+v err=%v", got, err)
	}
}

func TestAttachReportsDuplicateAndInvalid(t *testing.T) {
	env := newServiceEnv(t)
	listing := env.publishListing(t, env.other)
	profile := env.putProfile(t, env.other)
	first := env.mustReport(t, listing, ReasonSpam)
	second := env.mustReport(t, listing, ReasonHarassment)
	otherSubject, err := env.svc.Create(context.Background(), env.reporter, CreateInput{
		TargetType: TargetPublicProfile, TargetID: profile, ReasonCode: ReasonImpersonation,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: listing, Title: "queue", ReportIDs: []ID{first.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.AttachReports(context.Background(), created.Case.ID, AttachReportsInput{ReportIDs: []ID{first.ID}}); !errors.Is(err, errConflict) {
		t.Fatalf("duplicate err = %v", err)
	}
	if _, err := env.svc.AttachReports(context.Background(), created.Case.ID, AttachReportsInput{ReportIDs: []ID{second.ID, second.ID}}); !errors.Is(err, errConflict) {
		t.Fatalf("same-request dup err = %v", err)
	}
	if _, err := env.svc.AttachReports(context.Background(), created.Case.ID, AttachReportsInput{ReportIDs: []ID{otherSubject.ID}}); !errors.Is(err, errInvalidAttachment) {
		t.Fatalf("subject mismatch err = %v", err)
	}
	if _, err := env.svc.AttachReports(context.Background(), created.Case.ID, AttachReportsInput{ReportIDs: []ID{mustID(t)}}); !errors.Is(err, errNotFound) {
		t.Fatalf("missing report err = %v", err)
	}
	otherCase, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: listing, Title: "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.AttachReports(context.Background(), otherCase.Case.ID, AttachReportsInput{ReportIDs: []ID{first.ID}}); !errors.Is(err, errConflict) {
		t.Fatalf("active case conflict err = %v", err)
	}
	attached, err := env.svc.AttachReports(context.Background(), created.Case.ID, AttachReportsInput{ReportIDs: []ID{second.ID}})
	if err != nil || len(attached.ReportIDs) != 2 {
		t.Fatalf("attach second = %+v err=%v", attached, err)
	}
}

func TestCaseStatusTransitions(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetPublicProfile, SubjectID: subject, Title: "profile",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Case.ID
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusResolved}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("open to resolved err = %v", err)
	}
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusClosed}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("open to closed err = %v", err)
	}
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusInvestigating}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusOpen}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("back to open err = %v", err)
	}
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusResolved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusClosed}); err != nil {
		t.Fatal(err)
	}
	got, err := env.svc.GetCase(context.Background(), id)
	if err != nil || got.Case.Status != CaseStatusClosed {
		t.Fatalf("closed = %+v err=%v", got, err)
	}
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusResolved}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
}

func TestCasePriorityAssignmentAndHistoryAppend(t *testing.T) {
	env := newServiceEnv(t)
	subject := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: subject, Title: "priority", Priority: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Case.ID
	if _, err := env.svc.SetCasePriority(context.Background(), id, CasePriorityInput{Priority: "critical"}); !errors.Is(err, errInvalidPriority) {
		t.Fatalf("bad priority err = %v", err)
	}
	env.now = env.now.Add(time.Second)
	actor := mustID(t)
	updated, err := env.svc.SetCasePriority(context.Background(), id, CasePriorityInput{Priority: CasePriorityUrgent, ActorID: &actor})
	if err != nil || updated.Case.Priority != CasePriorityUrgent {
		t.Fatalf("priority = %+v err=%v", updated, err)
	}
	env.now = env.now.Add(time.Second)
	assignee := mustID(t)
	assigned, err := env.svc.AssignCase(context.Background(), id, CaseAssignmentInput{AssignedStaffID: &assignee})
	if err != nil || assigned.Case.AssignedStaffID == nil || *assigned.Case.AssignedStaffID != assignee {
		t.Fatalf("assign = %+v err=%v", assigned, err)
	}
	env.now = env.now.Add(time.Second)
	noted, err := env.svc.AddCaseNote(context.Background(), id, CaseNoteInput{Note: "  keep watching  "})
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]CaseHistoryKind, 0, len(noted.History))
	for _, row := range noted.History {
		kinds = append(kinds, row.Kind)
	}
	want := []CaseHistoryKind{HistoryCaseCreated, HistoryPriorityChanged, HistoryAssignmentChanged, HistoryStaffNoteAdded}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v want %v", kinds, want)
		}
		if i > 0 && noted.History[i].CreatedAt.Before(noted.History[i-1].CreatedAt) {
			t.Fatalf("history not ordered: %+v", noted.History)
		}
	}
	if noted.History[3].Note == nil || *noted.History[3].Note != "keep watching" {
		t.Fatalf("note = %+v", noted.History[3].Note)
	}
	if noted.History[1].ActorStaffID == nil || *noted.History[1].ActorStaffID != actor {
		t.Fatalf("priority actor = %+v", noted.History[1].ActorStaffID)
	}
	if noted.History[2].ActorStaffID != nil {
		t.Fatalf("assignment invented actor = %+v", noted.History[2].ActorStaffID)
	}
}

func TestListCasesDeterministicBoundedFilters(t *testing.T) {
	env := newServiceEnv(t)
	listing := mustID(t)
	profile := mustID(t)
	first, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: listing, Title: "a", Priority: "low",
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Minute)
	second, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetPublicProfile, SubjectID: profile, Title: "b", Priority: "urgent",
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := env.svc.ListCases(context.Background(), CaseQuery{Limit: 1})
	if err != nil || len(page.Cases) != 1 || page.Cases[0].ID != second.Case.ID || page.NextCursor == "" {
		t.Fatalf("page = %+v err=%v", page, err)
	}
	page2, err := env.svc.ListCases(context.Background(), CaseQuery{Limit: 1, Cursor: page.NextCursor})
	if err != nil || len(page2.Cases) != 1 || page2.Cases[0].ID != first.Case.ID {
		t.Fatalf("page2 = %+v err=%v", page2, err)
	}
	st := CaseStatusOpen
	pr := CasePriorityLow
	tt := TargetListing
	filtered, err := env.svc.ListCases(context.Background(), CaseQuery{Status: &st, Priority: &pr, SubjectType: &tt})
	if err != nil || len(filtered.Cases) != 1 || filtered.Cases[0].ID != first.Case.ID {
		t.Fatalf("filtered = %+v err=%v", filtered, err)
	}
	if _, err := env.svc.ListCases(context.Background(), CaseQuery{Limit: MaxCaseLimit + 1}); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("oversize err = %v", err)
	}
	bad := CaseStatus("triaged")
	if _, err := env.svc.ListCases(context.Background(), CaseQuery{Status: &bad}); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("bad status filter err = %v", err)
	}
}

func (e *serviceEnv) mustReport(t *testing.T, listing ID, reason ReasonCode) Report {
	t.Helper()
	row, err := e.svc.Create(context.Background(), e.reporter, CreateInput{
		TargetType: TargetListing, TargetID: listing, ReasonCode: reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	return row
}
