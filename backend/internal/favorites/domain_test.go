package favorites

import (
	"errors"
	"testing"
)

func TestParseIDRejectsZeroAndGarbage(t *testing.T) {
	if _, err := ParseID(""); !errors.Is(err, errZeroID) {
		t.Fatalf("empty err = %v", err)
	}
	if _, err := ParseID("not-a-uuid"); !errors.Is(err, errZeroID) {
		t.Fatalf("garbage err = %v", err)
	}
	zero := "00000000-0000-0000-0000-000000000000"
	if _, err := ParseID(zero); !errors.Is(err, errZeroID) {
		t.Fatalf("zero err = %v", err)
	}
}

func TestFavoriteValidate(t *testing.T) {
	user, listing := mustID(t), mustID(t)
	if err := (Favorite{UserID: user, ListingID: listing}).Validate(); err == nil {
		t.Fatal("expected missing created_at to fail")
	}
	if err := (Favorite{}).Validate(); !errors.Is(err, errZeroID) {
		t.Fatalf("zero ids err = %v", err)
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
