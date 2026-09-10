package contracts

import (
	"context"
	"errors"
	"time"
)

const (
	InteractionTypeListingInspection = "listing_inspection"
	InteractionTypeTransaction       = "transaction"
	InteractionTypeDelivery          = "delivery"
)

var (
	ErrZeroID      = errors.New("verified id must not be zero")
	ErrNotFound    = errors.New("verified interaction not found")
	ErrUnavailable = errors.New("verified unavailable")
)

// ID is an interaction, listing, or user UUID. Other domains must not treat
// this as a Verified table handle.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// InteractionRef is the cross-domain read surface for a completed verified interaction.
// Existence of this record means the interaction is completed/verified.
type InteractionRef struct {
	ID              ID
	ListingID       ID
	RequesterUserID ID
	ProviderUserID  ID
	InteractionType string
	VerifiedAt      time.Time
}

// Interactions is the only Verified read surface Reviews may call.
// Implementations live in the verified package; callers must not import that package.
type Interactions interface {
	GetInteraction(ctx context.Context, id ID) (InteractionRef, error)
}
