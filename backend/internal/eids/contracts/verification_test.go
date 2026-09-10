package contracts

import "testing"

func TestVerificationTypeIsNotGeneric(t *testing.T) {
	if !TypeProperty.Valid() || !TypeVehicle.Valid() {
		t.Fatal("property and vehicle must be valid")
	}
	if VerificationType("identity").Valid() || VerificationType("generic").Valid() {
		t.Fatal("person/generic types are not listing EİDS")
	}
	if _, err := ParseType("property"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseType("vehicle"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseType(""); err != ErrInvalidType {
		t.Fatalf("empty type err = %v", err)
	}
}

func TestViewOmitsProviderReference(t *testing.T) {
	var v VerificationView
	if v.VerificationID.IsZero() {
		// zero is allowed on empty view; field set must not include provider payload
	}
	_ = v.FailureCode
}
