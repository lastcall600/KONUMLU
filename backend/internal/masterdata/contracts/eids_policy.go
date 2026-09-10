package contracts

import (
	"context"
	"errors"
)

const (
	EIDSRequirementNone     EIDSRequirement = "none"
	EIDSRequirementProperty EIDSRequirement = "property"
	EIDSRequirementVehicle  EIDSRequirement = "vehicle"
)

type EIDSRequirement string

func (r EIDSRequirement) Valid() bool {
	switch r {
	case EIDSRequirementNone, EIDSRequirementProperty, EIDSRequirementVehicle:
		return true
	default:
		return false
	}
}

func (r EIDSRequirement) Required() bool {
	return r == EIDSRequirementProperty || r == EIDSRequirementVehicle
}

var ErrInvalidEIDSRequirement = errors.New("invalid eids category requirement")

// EIDSRequirementLookup is the server-side category policy Listings/EİDS may call.
// Clients cannot set this value.
type EIDSRequirementLookup interface {
	Requirement(ctx context.Context, categoryID ID) (EIDSRequirement, error)
}
