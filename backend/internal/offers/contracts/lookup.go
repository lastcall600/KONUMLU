package contracts

import (
	"context"
	"errors"
)

var (
	ErrZeroID      = errors.New("offers id must not be zero")
	ErrNotFound    = errors.New("offer not found")
	ErrUnavailable = errors.New("offers unavailable")
)

// ID is an Offer, Need, business, service, or provider UUID.
// Other domains must not treat this as a table handle.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// Price is optional agreed amount copied from an Offer. It has no tax or payment semantics.
type Price struct {
	Amount   string
	Currency string
}

// OfferRef is the cross-domain Offer read surface.
// ProviderUserID is internal. Callers must not expose it on consumer HTTP.
type OfferRef struct {
	ID                 ID
	NeedID             ID
	ProviderBusinessID ID
	ServiceID          ID
	ProviderUserID     ID
	Price              *Price
	Status             string
}

// Lookup is the Offers-owned read surface. Callers must not import offers impl.
type Lookup interface {
	GetOffer(ctx context.Context, offerID ID) (OfferRef, error)
}
