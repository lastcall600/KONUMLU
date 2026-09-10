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

func TestDeliveryRefCarriesIDsWithoutParties(t *testing.T) {
	var deliveryID, txnID ID
	deliveryID[0] = 1
	txnID[0] = 2
	ref := DeliveryRef{
		ID:            deliveryID,
		TransactionID: txnID,
		Eligibility:   EligibilityRequired,
		Status:        "pending",
	}
	if ref.ID.IsZero() || ref.TransactionID.IsZero() || ref.Eligibility != EligibilityRequired {
		t.Fatalf("ref = %+v", ref)
	}
}
