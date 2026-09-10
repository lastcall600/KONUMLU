package contracts

import "context"

// ListingEIDSSubject is the minimum listing surface EİDS may read.
// It does not expose listing content or verification status.
type ListingEIDSSubject struct {
	ID          ID
	OwnerUserID ID
	CategoryID  ID
}

// EIDSSubject is the Listings contract EİDS uses for owner and category.
// EİDS must not query listings tables.
type EIDSSubject interface {
	ResolveListingEIDSSubject(ctx context.Context, listingID ID) (ListingEIDSSubject, error)
}
