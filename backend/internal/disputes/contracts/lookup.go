package contracts

import (
	"context"
	"errors"
)

var (
	ErrZeroID      = errors.New("disputes id must not be zero")
	ErrNotFound    = errors.New("dispute not found")
	ErrUnavailable = errors.New("disputes unavailable")
)

// ID is a Dispute or Transaction UUID as understood by Disputes.
// Other domains must not treat this as a table handle.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// DisputeRef is the cross-domain dispute-lifecycle read surface.
// User identities are omitted. Resolution is an internal outcome label only.
type DisputeRef struct {
	ID             ID
	TransactionID  ID
	ReasonCode     string
	Status         string
	ResolutionCode *string
}

// Lookup is the Disputes-owned read surface.
// Callers must not import the disputes implementation package.
// V1 does not mutate Transactions, Payments, Deliveries, Trust, or Moderation.
type Lookup interface {
	GetDispute(ctx context.Context, disputeID ID) (DisputeRef, error)
}
