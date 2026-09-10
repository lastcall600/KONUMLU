package disputes

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreateOpenAndLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	d, err := CreateOpen(validCreate(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != StatusOpen || d.ResolvedAt != nil || d.OpenedByRole() != RoleRequester {
		t.Fatalf("d = %+v", d)
	}
	if _, _, err := d.Resolve(ResolutionNoAction, now.Add(time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("open resolve err = %v", err)
	}
	if _, _, err := d.Close(now.Add(time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("open close err = %v", err)
	}
	review, changed, err := d.StartReview(now.Add(time.Minute))
	if err != nil || !changed || review.Status != StatusUnderReview {
		t.Fatalf("review = %+v changed=%v err=%v", review, changed, err)
	}
	same, changed, err := review.StartReview(now.Add(2 * time.Minute))
	if err != nil || changed || same.Status != StatusUnderReview {
		t.Fatalf("idempotent review = %+v changed=%v err=%v", same, changed, err)
	}
	if _, _, err := review.Withdraw(now.Add(2 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("under_review withdraw err = %v", err)
	}
	resolved, changed, err := review.Resolve(ResolutionBuyerFavored, now.Add(3*time.Minute))
	if err != nil || !changed || resolved.Status != StatusResolved || resolved.ResolvedAt == nil {
		t.Fatalf("resolved = %+v err=%v", resolved, err)
	}
	if _, _, err := resolved.Resolve(ResolutionProviderFavored, now.Add(4*time.Minute)); !errors.Is(err, errConflict) {
		t.Fatalf("different resolution err = %v", err)
	}
	again, changed, err := resolved.Resolve(ResolutionBuyerFavored, now.Add(4*time.Minute))
	if err != nil || changed || again.Status != StatusResolved {
		t.Fatalf("same resolution replay = %+v changed=%v err=%v", again, changed, err)
	}
	if _, _, err := resolved.Withdraw(now.Add(4 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("resolved withdraw err = %v", err)
	}
	closed, changed, err := resolved.Close(now.Add(5 * time.Minute))
	if err != nil || !changed || closed.Status != StatusClosed {
		t.Fatalf("closed = %+v err=%v", closed, err)
	}
	if _, _, err := closed.StartReview(now.Add(6 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
}

func TestWithdrawFromOpenOnly(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	d, err := CreateOpen(validCreate(t), now)
	if err != nil {
		t.Fatal(err)
	}
	withdrawn, changed, err := d.Withdraw(now.Add(time.Minute))
	if err != nil || !changed || withdrawn.Status != StatusWithdrawn {
		t.Fatalf("withdrawn = %+v err=%v", withdrawn, err)
	}
	same, changed, err := withdrawn.Withdraw(now.Add(2 * time.Minute))
	if err != nil || changed || same.Status != StatusWithdrawn {
		t.Fatalf("idempotent withdraw = %+v changed=%v err=%v", same, changed, err)
	}
}

func TestInvalidReasonAndClientSpoofFieldsRejectedAtDomain(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	cmd := validCreate(t)
	cmd.ReasonCode = "chargeback"
	if _, err := CreateOpen(cmd, now); !errors.Is(err, errInvalidReason) {
		t.Fatalf("reason err = %v", err)
	}
	cmd = validCreate(t)
	cmd.OpenedByUserID = mustID(t)
	if _, err := CreateOpen(cmd, now); !errors.Is(err, errInvalidDispute) {
		t.Fatalf("stranger opener err = %v", err)
	}
	long := strings.Repeat("a", MaxStatementRunes+1)
	cmd = validCreate(t)
	cmd.Statement = &long
	if _, err := CreateOpen(cmd, now); !errors.Is(err, errInvalidStatement) {
		t.Fatalf("statement err = %v", err)
	}
}

func TestEvidenceAppendModel(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	d, err := CreateOpen(validCreate(t), now)
	if err != nil {
		t.Fatal(err)
	}
	actor := d.OpenedByUserID
	e, err := CreateEvidence(EvidenceCommand{
		DisputeID:    d.ID,
		EvidenceType: EvidencePartyStatement,
		Title:        "photo note",
		ActorUserID:  &actor,
		ActorRole:    RoleRequester,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if e.ReferenceValue != nil {
		t.Fatal("party_statement must not carry reference")
	}
	if _, err := CreateEvidence(EvidenceCommand{
		DisputeID:    d.ID,
		EvidenceType: EvidenceInternalReference,
		Title:        "staff",
		ActorUserID:  &actor,
		ActorRole:    RoleRequester,
	}, now); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("party internal err = %v", err)
	}
	ref := "ext-1"
	internal, err := CreateEvidence(EvidenceCommand{
		DisputeID:      d.ID,
		EvidenceType:   EvidenceInternalReference,
		Title:          "note",
		ReferenceValue: &ref,
		ActorRole:      RoleInternal,
	}, now)
	if err != nil || internal.ActorUserID != nil {
		t.Fatalf("internal = %+v err=%v", internal, err)
	}
}

func validCreate(t *testing.T) CreateCommand {
	t.Helper()
	requester := mustID(t)
	provider := mustID(t)
	return CreateCommand{
		TransactionID:   mustID(t),
		RequesterUserID: requester,
		ProviderUserID:  provider,
		OpenedByUserID:  requester,
		ReasonCode:      ReasonItemOrServiceNotAsDescribed,
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
