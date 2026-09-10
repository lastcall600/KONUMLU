package eidsadapter

import (
	"context"

	"backend/internal/eids"
)

// Unconfigured is the production-safe gateway when no official adapter is wired.
// It never returns verified. Provider unavailability must not bypass EİDS.
type Unconfigured struct{}

func (Unconfigured) VerifyProperty(context.Context, eids.PropertyVerifyRequest) (eids.ProviderOutcome, error) {
	return eids.ProviderOutcome{Outcome: eids.OutcomeUnavailable}, nil
}

func (Unconfigured) VerifyVehicle(context.Context, eids.VehicleVerifyRequest) (eids.ProviderOutcome, error) {
	return eids.ProviderOutcome{Outcome: eids.OutcomeUnavailable}, nil
}

var _ eids.Gateway = Unconfigured{}
