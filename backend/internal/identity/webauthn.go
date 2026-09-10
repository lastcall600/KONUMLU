package identity

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// WebAuthnConfig is Relying Party configuration supplied from the environment.
// Production RP ID and origins must not be hardcoded.
type WebAuthnConfig struct {
	RPDisplayName string
	RPID          string
	RPOrigins     []string
}

func (c WebAuthnConfig) Validate() error {
	if strings.TrimSpace(c.RPDisplayName) == "" {
		return fmt.Errorf("%w: rp display name is required", errInvalidWebAuthnConfig)
	}
	rpid := strings.TrimSpace(c.RPID)
	if rpid == "" {
		return fmt.Errorf("%w: rp id is required", errInvalidWebAuthnConfig)
	}
	if strings.Contains(rpid, "://") || strings.ContainsAny(rpid, "/\\") {
		return fmt.Errorf("%w: rp id must be a domain, not an origin", errInvalidWebAuthnConfig)
	}
	if len(c.RPOrigins) == 0 {
		return fmt.Errorf("%w: at least one rp origin is required", errInvalidWebAuthnConfig)
	}
	seen := make(map[string]struct{}, len(c.RPOrigins))
	for _, origin := range c.RPOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			return fmt.Errorf("%w: rp origin must not be empty", errInvalidWebAuthnConfig)
		}
		u, err := url.Parse(origin)
		if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("%w: rp origin is invalid", errInvalidWebAuthnConfig)
		}
		if _, dup := seen[origin]; dup {
			return fmt.Errorf("%w: duplicate rp origin", errInvalidWebAuthnConfig)
		}
		seen[origin] = struct{}{}
	}
	return nil
}

func (c WebAuthnConfig) normalized() WebAuthnConfig {
	origins := make([]string, 0, len(c.RPOrigins))
	for _, origin := range c.RPOrigins {
		origins = append(origins, strings.TrimSpace(origin))
	}
	return WebAuthnConfig{
		RPDisplayName: strings.TrimSpace(c.RPDisplayName),
		RPID:          strings.TrimSpace(c.RPID),
		RPOrigins:     origins,
	}
}

// NewWebAuthn builds a go-webauthn Relying Party from injected config.
// It is not wired at process startup; callers initialize it when needed.
func NewWebAuthn(cfg WebAuthnConfig) (*webauthn.WebAuthn, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.normalized()
	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName: cfg.RPDisplayName,
		RPID:          cfg.RPID,
		RPOrigins:     cfg.RPOrigins,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidWebAuthnConfig, err)
	}
	return wa, nil
}

// PasskeyUser adapts an Identity account and passkeys to go-webauthn.
// Name and DisplayName are ceremony display fields, not Users-domain profile data.
type PasskeyUser struct {
	ID          ID
	Name        string
	DisplayName string
	Passkeys    []PasskeyCredential
}

var _ webauthn.User = PasskeyUser{}

func (u PasskeyUser) WebAuthnID() []byte {
	id := u.ID
	out := make([]byte, len(id))
	copy(out, id[:])
	return out
}

func (u PasskeyUser) WebAuthnName() string {
	return u.Name
}

func (u PasskeyUser) WebAuthnDisplayName() string {
	return u.DisplayName
}

func (u PasskeyUser) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(u.Passkeys))
	for _, c := range u.Passkeys {
		out = append(out, c.WebAuthnCredential())
	}
	return out
}

// WebAuthnCredential maps durable Identity passkey material to the library credential record.
func (c PasskeyCredential) WebAuthnCredential() webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, 0, len(c.Transports))
	for _, t := range c.Transports {
		transports = append(transports, protocol.AuthenticatorTransport(t))
	}
	return webauthn.Credential{
		ID:        cloneBytes(c.CredentialID),
		PublicKey: cloneBytes(c.PublicKey),
		Transport: transports,
		Flags: webauthn.CredentialFlags{
			BackupEligible: c.BackupEligible,
			BackupState:    c.BackupState,
		},
		Authenticator: webauthn.Authenticator{
			SignCount: uint32(c.SignCount),
		},
	}
}
