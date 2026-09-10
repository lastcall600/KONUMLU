package contracts

import (
	"context"
	"errors"
)

var (
	ErrZeroID       = errors.New("listings id must not be zero")
	ErrNotFound     = errors.New("listing not found")
	ErrForbidden    = errors.New("listing access denied")
	ErrInvalidEvent = errors.New("invalid listing event")
)

// ID is a listing or user UUID. Other domains must not treat this as a Listings table handle.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// ListingRef is the minimal cross-domain listing read surface.
// Status is owner lifecycle; ModerationState is staff visibility enforcement.
type ListingRef struct {
	ID              ID
	OwnerUserID     ID
	Status          string
	ModerationState string
}

// Ownership is the Listings owner/status read surface. Search uses SearchSource.
// Staff hide/remove/restore uses ModerationEnforcement. Callers must not import listings impl.
type Ownership interface {
	ResolveListingOwner(ctx context.Context, listingID ID) (ListingRef, error)
	AssertListingOwnedBy(ctx context.Context, listingID, userID ID) error
}
