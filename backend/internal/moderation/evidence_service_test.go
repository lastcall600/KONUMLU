package moderation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAddEvidenceSupportedTypes(t *testing.T) {
	env := newServiceEnv(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: mustID(t), Title: "evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Case.ID
	note, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: "  intake  ", Description: " looks coordinated ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if note.EvidenceType != EvidenceStaffNote || note.Title != "intake" || note.Description == nil || *note.Description != "looks coordinated" {
		t.Fatalf("note = %+v", note)
	}
	if note.ReferenceValue != nil || note.ActorStaffID != nil {
		t.Fatalf("note extras = %+v", note)
	}
	env.now = env.now.Add(time.Second)
	ext, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceExternalReference, Title: "source", ReferenceValue: "https://example.test/item/1",
	})
	if err != nil || ext.ReferenceValue == nil || *ext.ReferenceValue != "https://example.test/item/1" {
		t.Fatalf("external = %+v err=%v", ext, err)
	}
	env.now = env.now.Add(time.Second)
	internal, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceInternalReference, Title: "ticket", ReferenceValue: "OPS-441",
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	snap, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceSnapshotReference, Title: "listing snapshot", ReferenceValue: "snap:listing:abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := env.svc.ListEvidence(context.Background(), id, 0)
	if err != nil || len(rows) != 4 {
		t.Fatalf("list = %+v err=%v", rows, err)
	}
	if rows[0].ID != note.ID || rows[1].ID != ext.ID || rows[2].ID != internal.ID || rows[3].ID != snap.ID {
		t.Fatalf("order = %+v", rows)
	}
}

func TestEvidenceTypeFieldValidation(t *testing.T) {
	env := newServiceEnv(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: mustID(t), Title: "validate",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Case.ID
	if _, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: "screenshot", Title: "nope",
	}); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("unknown type err = %v", err)
	}
	if _, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: "note", ReferenceValue: "https://example.test",
	}); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("staff_note reference err = %v", err)
	}
	if _, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceExternalReference, Title: "url",
	}); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("external missing ref err = %v", err)
	}
	if _, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceInternalReference, Title: "ref",
	}); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("internal missing ref err = %v", err)
	}
	if _, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceSnapshotReference, Title: "snap",
	}); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("snapshot missing ref err = %v", err)
	}
	if _, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: strings.Repeat("a", MaxEvidenceTitleBytes+1),
	}); !errors.Is(err, errInvalidBody) {
		t.Fatalf("title oversize err = %v", err)
	}
	if _, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceExternalReference, Title: "jwt", ReferenceValue: "eyJhbGciOiJIUzI1NiJ9.e30.sig",
	}); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("secret jwt err = %v", err)
	}
	if _, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: "cookie dump", Description: "Authorization: Bearer secret",
	}); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("auth material err = %v", err)
	}
}

func TestEvidenceAppendOnlyAndHistoryAtomic(t *testing.T) {
	env := newServiceEnv(t)
	actor := mustID(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetPublicProfile, SubjectID: mustID(t), Title: "atomic", ActorID: &actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Case.ID
	env.now = env.now.Add(time.Second)
	first, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: "first", ActorID: &actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.now = env.now.Add(time.Second)
	second, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceInternalReference, Title: "second", ReferenceValue: "INT-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := env.svc.ListEvidence(context.Background(), id, 10)
	if err != nil || len(rows) != 2 || rows[0].Title != "first" || rows[1].Title != "second" {
		t.Fatalf("append list = %+v err=%v", rows, err)
	}
	if rows[0].ID != first.ID || rows[0].Title != first.Title || rows[0].ActorStaffID == nil || *rows[0].ActorStaffID != actor {
		t.Fatalf("first mutated = %+v", rows[0])
	}
	got, err := env.svc.GetCase(context.Background(), id)
	if err != nil || len(got.History) != 3 {
		t.Fatalf("history = %+v err=%v", got.History, err)
	}
	if got.History[1].Kind != HistoryEvidenceAdded || got.History[1].EvidenceID == nil || *got.History[1].EvidenceID != first.ID {
		t.Fatalf("first hist = %+v", got.History[1])
	}
	if got.History[1].ActorStaffID == nil || *got.History[1].ActorStaffID != actor {
		t.Fatalf("first hist actor = %+v", got.History[1].ActorStaffID)
	}
	if got.History[2].Kind != HistoryEvidenceAdded || got.History[2].EvidenceID == nil || *got.History[2].EvidenceID != second.ID {
		t.Fatalf("second hist = %+v", got.History[2])
	}
	if got.History[2].ActorStaffID != nil {
		t.Fatalf("invented actor = %+v", got.History[2].ActorStaffID)
	}

	env.store.SetFail(errUnavailable)
	if _, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: "fail",
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("fail err = %v", err)
	}
	env.store.SetFail(nil)
	afterFail, err := env.svc.ListEvidence(context.Background(), id, 10)
	if err != nil || len(afterFail) != 2 {
		t.Fatalf("partial evidence = %+v err=%v", afterFail, err)
	}
	hist, err := env.svc.GetCase(context.Background(), id)
	if err != nil || len(hist.History) != 3 {
		t.Fatalf("partial history = %+v err=%v", hist.History, err)
	}
}

func TestEvidenceClosedCaseMatchesHistoryPolicy(t *testing.T) {
	env := newServiceEnv(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: mustID(t), Title: "closed",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Case.ID
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusInvestigating}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusResolved}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.TransitionCase(context.Background(), id, CaseTransitionInput{To: CaseStatusClosed}); err != nil {
		t.Fatal(err)
	}
	row, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: "after close",
	})
	if err != nil || row.CaseID != id {
		t.Fatalf("closed evidence = %+v err=%v", row, err)
	}
}

func TestEvidenceWrongAndMissingCase(t *testing.T) {
	env := newServiceEnv(t)
	if _, err := env.svc.AddEvidence(context.Background(), mustID(t), AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: "ghost",
	}); !errors.Is(err, errNotFound) {
		t.Fatalf("missing add err = %v", err)
	}
	if _, err := env.svc.ListEvidence(context.Background(), mustID(t), 10); !errors.Is(err, errNotFound) {
		t.Fatalf("missing list err = %v", err)
	}
	if _, err := env.svc.AddEvidence(context.Background(), ID{}, AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: "zero",
	}); !errors.Is(err, errZeroID) {
		t.Fatalf("zero err = %v", err)
	}
	if _, err := env.svc.ListEvidence(context.Background(), mustID(t), MaxEvidenceLimit+1); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("limit err = %v", err)
	}
}

func TestEvidenceListDeterministicOrder(t *testing.T) {
	env := newServiceEnv(t)
	detail, err := env.svc.CreateCase(context.Background(), CreateCaseInput{
		SubjectType: TargetListing, SubjectID: mustID(t), Title: "order",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := detail.Case.ID
	same := env.now
	first, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := env.svc.AddEvidence(context.Background(), id, AddEvidenceInput{
		EvidenceType: EvidenceStaffNote, Title: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.CreatedAt.Equal(same) || !second.CreatedAt.Equal(same) {
		t.Fatalf("expected same created_at first=%v second=%v clock=%v", first.CreatedAt, second.CreatedAt, same)
	}
	rows, err := env.svc.ListEvidence(context.Background(), id, 50)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %+v err=%v", rows, err)
	}
	if bytesCmp(rows[0].ID, rows[1].ID) >= 0 {
		t.Fatalf("id order = %s then %s", rows[0].ID, rows[1].ID)
	}
}

func bytesCmp(a, b ID) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
