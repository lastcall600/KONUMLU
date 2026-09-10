package contracts

import "testing"

func TestPublishedCategoryLookupIDZero(t *testing.T) {
	var id ID
	if !id.IsZero() {
		t.Fatal("zero ID must report IsZero")
	}
	id[0] = 1
	if id.IsZero() {
		t.Fatal("non-zero ID")
	}
}
