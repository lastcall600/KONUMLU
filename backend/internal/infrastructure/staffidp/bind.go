package staffidp

import (
	"errors"

	"backend/internal/platform/config"
	"backend/internal/staffauth/contracts"
)

var (
	errMisconfigured   = contracts.ErrMisconfigured
	errAdapterRequired = contracts.ErrAdapterRequired
)

var (
	ErrMisconfigured   = errMisconfigured
	ErrAdapterRequired = errAdapterRequired
)

// Bind returns a staff IdentityProvider for production OIDC wiring.
//
// Empty STAFF_IDP_* config: no provider (staff routes stay unregistered).
// Partial config: fail closed.
// Complete config: a registered adapter is required. No vendor is selected yet,
// so production passes a nil adapter and wiring fails closed.
// Development fixtures must use Resolve, not this function.
func Bind(cfg config.StaffIDP, adapter contracts.IdentityProvider) (contracts.IdentityProvider, error) {
	if cfg.Empty() {
		if adapter != nil {
			return nil, errors.New("staff identity adapter must not be registered without STAFF_IDP configuration")
		}
		return nil, nil
	}
	if !cfg.Complete() {
		return nil, errMisconfigured
	}
	if adapter == nil {
		return nil, errAdapterRequired
	}
	return adapter, nil
}
