package contracts

import (
	"errors"
	"testing"
)

func TestNewListingWriteScope(t *testing.T) {
	if _, err := NewListingWriteScope(ID{}); !errors.Is(err, ErrZeroID) {
		t.Fatalf("err = %v", err)
	}
	var id ID
	id[0] = 1
	scope, err := NewListingWriteScope(id)
	if err != nil || scope.ListingID != id {
		t.Fatalf("scope = %+v err = %v", scope, err)
	}
}
