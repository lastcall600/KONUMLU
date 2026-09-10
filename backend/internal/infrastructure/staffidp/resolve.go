package staffidp

import (
	"errors"

	"backend/internal/platform/config"
	"backend/internal/staffauth/contracts"
)

var (
	ErrDevForbidden = errors.New("STAFF_DEV_IDP is development-only")
	ErrDevConflict  = errors.New("STAFF_DEV_IDP cannot be combined with STAFF_IDP")
)

// Resolve selects the staff IdentityProvider for process wiring.
//
// Development/test: an explicit complete StaffDevIDP yields the local adapter.
// Production-like environments reject any StaffDevIDP.
// Missing production adapter remains Bind's fail-closed behavior.
// StaffDevIDP is never a fallback for missing STAFF_IDP.
func Resolve(cfg config.Config) (contracts.IdentityProvider, error) {
	if cfg.ProductionLike() && !cfg.StaffDevIDP.Empty() {
		return nil, ErrDevForbidden
	}
	if cfg.StaffDevIDP.Enabled {
		if !cfg.StaffIDP.Empty() {
			return nil, ErrDevConflict
		}
		return NewDev(cfg.StaffDevIDP)
	}
	return Bind(cfg.StaffIDP, nil)
}
