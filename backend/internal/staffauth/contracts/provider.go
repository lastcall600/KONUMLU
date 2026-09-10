package contracts

import (
	"context"
	"net/http"
)

// Credential is a provider-neutral staff proof. The raw token must never be
// persisted or written to logs.
type Credential struct {
	Token string
}

// Principal is an authenticated staff actor. Roles and attributes come from the
// identity provider after token validation, never from the client.
type Principal struct {
	StaffID    ID
	Roles      []Role
	Attributes map[string]string
}

func (p Principal) Clone() Principal {
	out := Principal{StaffID: p.StaffID}
	if len(p.Roles) > 0 {
		out.Roles = append([]Role(nil), p.Roles...)
	}
	if len(p.Attributes) > 0 {
		out.Attributes = make(map[string]string, len(p.Attributes))
		for k, v := range p.Attributes {
			out.Attributes[k] = v
		}
	}
	return out
}

// IdentityProvider verifies a staff credential. Implementations must validate
// issuer, audience, expiry, and subject when a token implementation exists.
// This port must not consult consumer Identity sessions.
type IdentityProvider interface {
	Verify(ctx context.Context, cred Credential) (Principal, error)
}

// Authorizer authenticates a staff request and evaluates RBAC permissions.
// Policy evaluation is separate from provider token validation.
type Authorizer interface {
	Authenticate(*http.Request) (Principal, error)
	Allows(Principal, Permission) bool
}
