package staffidp

import (
	"context"
	"crypto/subtle"

	"backend/internal/platform/config"
	"backend/internal/staffauth/contracts"
)

// DevProvider is an explicit development/test Staff Identity Provider.
// It authenticates a runtime Bearer token against server-side fixture
// identity and roles. It never reads privilege headers or consumer sessions.
type DevProvider struct {
	token   string
	staffID contracts.ID
	roles   []contracts.Role
}

func NewDev(cfg config.StaffDevIDP) (*DevProvider, error) {
	if !cfg.Enabled {
		return nil, contracts.ErrUnavailable
	}
	if !cfg.Complete() {
		return nil, contracts.ErrMisconfigured
	}
	id, err := contracts.ParseID(cfg.StaffID)
	if err != nil {
		return nil, contracts.ErrMisconfigured
	}
	roles := make([]contracts.Role, 0, len(cfg.Roles))
	seen := make(map[contracts.Role]struct{}, len(cfg.Roles))
	for _, raw := range cfg.Roles {
		role, ok := contracts.ParseRole(raw)
		if !ok {
			return nil, contracts.ErrMisconfigured
		}
		if _, exists := seen[role]; exists {
			continue
		}
		seen[role] = struct{}{}
		roles = append(roles, role)
	}
	if len(roles) == 0 {
		return nil, contracts.ErrMisconfigured
	}
	return &DevProvider{
		token:   cfg.Token,
		staffID: id,
		roles:   roles,
	}, nil
}

func (p *DevProvider) Verify(_ context.Context, cred contracts.Credential) (contracts.Principal, error) {
	if p == nil || p.token == "" || p.staffID.IsZero() {
		return contracts.Principal{}, contracts.ErrUnavailable
	}
	if cred.Token == "" {
		return contracts.Principal{}, contracts.ErrUnauthenticated
	}
	if subtle.ConstantTimeCompare([]byte(cred.Token), []byte(p.token)) != 1 {
		return contracts.Principal{}, contracts.ErrUnauthenticated
	}
	return contracts.Principal{
		StaffID: p.staffID,
		Roles:   append([]contracts.Role(nil), p.roles...),
	}, nil
}
