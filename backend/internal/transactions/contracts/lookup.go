package contracts

import (
	"context"
	"errors"
	"time"
)

var (
	ErrZeroID      = errors.New("transactions id must not be zero")
	ErrNotFound    = errors.New("transaction not found")
	ErrUnavailable = errors.New("transactions unavailable")
)

// ID is a Transaction or user UUID as understood by Transactions.
// Other domains must not treat this as a table handle.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// Price is the agreed commercial amount copied from a Transaction.
// It has no tax, escrow, or payment-instrument semantics.
type Price struct {
	Amount   string
	Currency string
}

const (
	// DeliveryEligibilityRequired means a participant may request a logistics
	// Delivery record. It does not mean every Transaction has or must have one.
	DeliveryEligibilityRequired = "delivery_required"
	// DeliveryEligibilityNone is the default: no physical delivery is assumed.
	DeliveryEligibilityNone = "no_delivery_required"
)

// TransactionRef is the cross-domain commercial-agreement read surface.
// RequesterUserID is the payer. ProviderUserID is the payee.
// Both user IDs are internal. Callers must not expose them on consumer HTTP.
type TransactionRef struct {
	ID              ID
	RequesterUserID ID
	ProviderUserID  ID
	Price           *Price
	Status          string
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

// DeliveryEligibility is the default logistics class of the commercial
// agreement. V1 Transactions do not store a delivery flag; the agreement
// itself is no_delivery_required until a participant creates a Delivery.
func (r TransactionRef) DeliveryEligibility() string {
	return DeliveryEligibilityNone
}

// MayCreateDelivery reports whether a participant may create a Delivery for
// this commercial agreement. It does not auto-create Delivery.
func (r TransactionRef) MayCreateDelivery() bool {
	switch r.Status {
	case "pending", "active":
		return true
	default:
		return false
	}
}

// Participant reports whether userID is requester or provider.
func (r TransactionRef) Participant(userID ID) bool {
	if userID.IsZero() {
		return false
	}
	return r.RequesterUserID == userID || r.ProviderUserID == userID
}

// MayOpenDispute reports whether a participant may open a V1 commercial dispute.
// Cancelled agreements are ineligible. This does not start refunds or reversals.
func (r TransactionRef) MayOpenDispute() bool {
	switch r.Status {
	case "pending", "active", "completed":
		return true
	default:
		return false
	}
}

// DisputeAnchor is the start of the V1 dispute window.
// Completed agreements use CompletedAt when present; otherwise CreatedAt.
func (r TransactionRef) DisputeAnchor() time.Time {
	if r.Status == "completed" && r.CompletedAt != nil && !r.CompletedAt.IsZero() {
		return r.CompletedAt.UTC()
	}
	return r.CreatedAt.UTC()
}

// Lookup is the Transactions-owned read surface used by Payments, Deliveries, and Disputes.
// Callers must not import the transactions implementation package.
type Lookup interface {
	GetTransaction(ctx context.Context, transactionID ID) (TransactionRef, error)
}
