package contracts

import (
	"testing"
	"time"
)

func TestLookupIDIsZero(t *testing.T) {
	var id ID
	if !id.IsZero() {
		t.Fatal("zero ID")
	}
	id[15] = 1
	if id.IsZero() {
		t.Fatal("non-zero ID")
	}
}

func TestTransactionRefCarriesPriceAndParties(t *testing.T) {
	var payer, payee ID
	payer[0] = 1
	payee[0] = 2
	ref := TransactionRef{
		ID:              payer,
		RequesterUserID: payer,
		ProviderUserID:  payee,
		Price:           &Price{Amount: "250.50", Currency: "TRY"},
		Status:          "pending",
	}
	if ref.Price == nil || ref.RequesterUserID == ref.ProviderUserID {
		t.Fatalf("ref = %+v", ref)
	}
}

func TestDeliveryEligibilityIsOptInNotForced(t *testing.T) {
	var requester, provider, stranger ID
	requester[0] = 1
	provider[0] = 2
	stranger[0] = 3
	pending := TransactionRef{RequesterUserID: requester, ProviderUserID: provider, Status: "pending"}
	if !pending.MayCreateDelivery() || pending.DeliveryEligibility() != DeliveryEligibilityNone {
		t.Fatalf("pending must be eligible to request delivery without forcing it: %+v", pending)
	}
	active := pending
	active.Status = "active"
	if !active.MayCreateDelivery() || active.DeliveryEligibility() != DeliveryEligibilityNone {
		t.Fatal("active should allow requested delivery without implying it is required")
	}
	for _, status := range []string{"cancelled", "completed", ""} {
		ref := pending
		ref.Status = status
		if ref.MayCreateDelivery() || ref.DeliveryEligibility() != DeliveryEligibilityNone {
			t.Fatalf("status %q eligibility = %s may=%v", status, ref.DeliveryEligibility(), ref.MayCreateDelivery())
		}
	}
	if !pending.Participant(requester) || !pending.Participant(provider) || pending.Participant(stranger) {
		t.Fatal("participant check")
	}
}

func TestMayOpenDisputeAndAnchor(t *testing.T) {
	created := mustParseTime(t, "2026-09-01T12:00:00Z")
	completed := mustParseTime(t, "2026-09-08T12:00:00Z")
	pending := TransactionRef{Status: "pending", CreatedAt: created}
	if !pending.MayOpenDispute() || !pending.DisputeAnchor().Equal(created) {
		t.Fatalf("pending dispute eligibility = %+v", pending)
	}
	active := pending
	active.Status = "active"
	if !active.MayOpenDispute() {
		t.Fatal("active should allow dispute")
	}
	done := pending
	done.Status = "completed"
	done.CompletedAt = &completed
	if !done.MayOpenDispute() || !done.DisputeAnchor().Equal(completed) {
		t.Fatalf("completed anchor = %v", done.DisputeAnchor())
	}
	cancelled := pending
	cancelled.Status = "cancelled"
	if cancelled.MayOpenDispute() {
		t.Fatal("cancelled must not open a dispute")
	}
}

func mustParseTime(t *testing.T, raw string) time.Time {
	t.Helper()
	got, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
