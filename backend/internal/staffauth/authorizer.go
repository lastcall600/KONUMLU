package staffauth

import (
	"errors"
	"net/http"
	"strings"

	"backend/internal/staffauth/contracts"
)

const bearerPrefix = "Bearer "

var privilegeHeaders = []string{
	"X-Staff-Role",
	"X-Staff-Roles",
	"X-Staff-Permission",
	"X-Staff-Permissions",
	"X-Staff-Id",
	"X-Staff-Actor",
}

// Authorizer validates staff credentials through IdentityProvider and evaluates
// RBAC separately. It never reads consumer session cookies or privilege headers.
type Authorizer struct {
	provider contracts.IdentityProvider
	policy   Policy
}

func NewAuthorizer(provider contracts.IdentityProvider, policy Policy) (*Authorizer, error) {
	if provider == nil {
		return nil, contracts.ErrUnavailable
	}
	return &Authorizer{provider: provider, policy: policy}, nil
}

func (a *Authorizer) Authenticate(r *http.Request) (contracts.Principal, error) {
	if a == nil || a.provider == nil {
		return contracts.Principal{}, contracts.ErrUnavailable
	}
	if r == nil {
		return contracts.Principal{}, contracts.ErrUnauthenticated
	}
	cred, err := credentialFromRequest(r)
	if err != nil {
		return contracts.Principal{}, err
	}
	principal, err := a.provider.Verify(r.Context(), cred)
	if err != nil {
		return contracts.Principal{}, mapProviderErr(err)
	}
	if principal.StaffID.IsZero() {
		return contracts.Principal{}, contracts.ErrForbidden
	}
	return principal.Clone(), nil
}

func (a *Authorizer) Allows(p contracts.Principal, perm contracts.Permission) bool {
	if a == nil {
		return false
	}
	return a.policy.Allows(p, perm)
}

func credentialFromRequest(r *http.Request) (contracts.Credential, error) {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if raw == "" || !strings.HasPrefix(raw, bearerPrefix) {
		return contracts.Credential{}, contracts.ErrUnauthenticated
	}
	token := strings.TrimSpace(strings.TrimPrefix(raw, bearerPrefix))
	if token == "" {
		return contracts.Credential{}, contracts.ErrUnauthenticated
	}
	return contracts.Credential{Token: token}, nil
}

func mapProviderErr(err error) error {
	switch {
	case err == nil:
		return contracts.ErrUnavailable
	case errors.Is(err, contracts.ErrUnauthenticated):
		return contracts.ErrUnauthenticated
	case errors.Is(err, contracts.ErrForbidden):
		return contracts.ErrForbidden
	case errors.Is(err, contracts.ErrMisconfigured):
		return contracts.ErrMisconfigured
	case errors.Is(err, contracts.ErrUnavailable):
		return contracts.ErrUnavailable
	default:
		return contracts.ErrUnavailable
	}
}

// PrivilegeHeadersIgnored documents headers that must never grant staff rights.
func PrivilegeHeadersIgnored() []string {
	return append([]string(nil), privilegeHeaders...)
}
