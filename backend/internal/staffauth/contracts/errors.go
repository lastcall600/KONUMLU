package contracts

import "errors"

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
	ErrUnavailable     = errors.New("staff auth unavailable")
	ErrMisconfigured   = errors.New("staff identity provider misconfigured")
	ErrAdapterRequired = errors.New("staff identity provider adapter required when STAFF_IDP is configured")
)
