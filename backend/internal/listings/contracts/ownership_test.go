package contracts

import (
	"errors"
	"testing"
)

func TestListingRefZeroID(t *testing.T) {
	var id ID
	if !id.IsZero() {
		t.Fatal("zero ID")
	}
	ref := ListingRef{}
	if !ref.ID.IsZero() || !ref.OwnerUserID.IsZero() {
		t.Fatal("empty ref must have zero IDs")
	}
	if !errors.Is(ErrForbidden, ErrForbidden) {
		t.Fatal("sentinel")
	}
}
