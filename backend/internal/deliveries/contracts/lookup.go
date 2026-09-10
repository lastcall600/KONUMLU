package contracts

import (
	"context"
	"errors"
)

var (
	ErrZeroID      = errors.New("deliveries id must not be zero")
	ErrNotFound    = errors.New("delivery not found")
	ErrUnavailable = errors.New("deliveries unavailable")
)

// ID is a Delivery or Transaction UUID as understood by Deliveries.
// Other domains must not treat this as a table handle.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

const (
	EligibilityRequired = "delivery_required"
	EligibilityNone     = "no_delivery_required"
)

// DeliveryRef is the cross-domain logistics-state read surface.
// Future Verified proof-of-delivery should reference ID (deliveryId).
// Callers must not duplicate Verified challenge logic here.
// User identities are omitted; resolve parties through Transactions Lookup.
type DeliveryRef struct {
	ID            ID
	TransactionID ID
	Eligibility   string
	Status        string
	Method        *string
}

// Lookup is the Deliveries-owned read surface.
// Callers must not import the deliveries implementation package.
// V1 does not wire Verified to this contract.
type Lookup interface {
	GetDelivery(ctx context.Context, deliveryID ID) (DeliveryRef, error)
	GetDeliveryByTransaction(ctx context.Context, transactionID ID) (DeliveryRef, error)
}
