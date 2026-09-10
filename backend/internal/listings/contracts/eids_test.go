package contracts

import "testing"

func TestListingEIDSSubjectIsNarrow(t *testing.T) {
	var id ID
	id[0] = 1
	sub := ListingEIDSSubject{ID: id, OwnerUserID: id, CategoryID: id}
	if sub.ID.IsZero() || sub.OwnerUserID.IsZero() || sub.CategoryID.IsZero() {
		t.Fatal("subject ids")
	}
}
