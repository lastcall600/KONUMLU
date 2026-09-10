package contracts

import (
	"testing"
)

func TestIDIsZero(t *testing.T) {
	var id ID
	if !id.IsZero() {
		t.Fatal("zero ID")
	}
	ref := ProfileRef{}
	if !ref.ID.IsZero() || !ref.OwnerUserID.IsZero() {
		t.Fatal("empty ref must have zero IDs")
	}
}
