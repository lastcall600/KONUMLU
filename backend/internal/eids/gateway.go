package eids

import (
	"context"
	"time"
)

// Gateway is the provider-neutral EİDS port. Type-specific methods keep
// property and vehicle semantics separate. Implementations live in
// infrastructure. Domain code must not invent official government fields.
type Gateway interface {
	VerifyProperty(ctx context.Context, in PropertyVerifyRequest) (ProviderOutcome, error)
	VerifyVehicle(ctx context.Context, in VehicleVerifyRequest) (ProviderOutcome, error)
}

type PropertyVerifyRequest struct {
	VerificationID ID
	ListingID      ID
}

type VehicleVerifyRequest struct {
	VerificationID ID
	ListingID      ID
}

type Outcome string

const (
	OutcomeVerified    Outcome = "verified"
	OutcomeFailed      Outcome = "failed"
	OutcomeUnavailable Outcome = "unavailable"
	OutcomeInProgress  Outcome = "in_progress"
)

func (o Outcome) valid() bool {
	switch o {
	case OutcomeVerified, OutcomeFailed, OutcomeUnavailable, OutcomeInProgress:
		return true
	default:
		return false
	}
}

// ProviderOutcome is the adapter mapping layer. It must not carry raw
// official payloads, credentials, or TCKN.
type ProviderOutcome struct {
	Outcome           Outcome
	ProviderReference string
	ExpiresAt         *time.Time
}
