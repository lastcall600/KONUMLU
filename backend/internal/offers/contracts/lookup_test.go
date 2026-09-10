package contracts

import "testing"

func TestIDIsZero(t *testing.T) {
	var id ID
	if !id.IsZero() {
		t.Fatal("zero ID")
	}
	id[15] = 1
	if id.IsZero() {
		t.Fatal("non-zero ID")
	}
}
