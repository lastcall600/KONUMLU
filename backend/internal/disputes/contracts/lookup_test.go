package contracts

import "testing"

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

func TestDisputeRefOmitsUserIdentities(t *testing.T) {
	var disputeID, txnID ID
	disputeID[0] = 1
	txnID[0] = 2
	code := "no_action"
	ref := DisputeRef{
		ID:             disputeID,
		TransactionID:  txnID,
		ReasonCode:     "other",
		Status:         "resolved",
		ResolutionCode: &code,
	}
	if ref.ID.IsZero() || ref.TransactionID.IsZero() || ref.ResolutionCode == nil {
		t.Fatalf("ref = %+v", ref)
	}
}
