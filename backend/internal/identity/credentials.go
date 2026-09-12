package identity

import (
	"context"
	"errors"
)

// CredentialGuard owns passkey security-center mutations and last-factor safety.
type CredentialGuard struct {
	passkeys  *Passkeys
	passwords *Passwords
	sessions  *Sessions
}

func NewCredentialGuard(passkeys *Passkeys, passwords *Passwords, sessions *Sessions) (*CredentialGuard, error) {
	if passkeys == nil || passwords == nil || sessions == nil {
		return nil, errStoreRequired
	}
	return &CredentialGuard{passkeys: passkeys, passwords: passwords, sessions: sessions}, nil
}

func (g *CredentialGuard) ListPasskeys(ctx context.Context, userID ID) ([]PasskeyCredential, error) {
	if userID.IsZero() {
		return nil, errZeroID
	}
	return g.passkeys.ListActiveForUser(ctx, userID)
}

func (g *CredentialGuard) RemovePasskey(ctx context.Context, actorUserID, credentialID ID) error {
	if actorUserID.IsZero() || credentialID.IsZero() {
		return errZeroID
	}
	cred, err := g.passkeys.Get(ctx, credentialID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return errNotFound
		}
		return err
	}
	if cred.UserID != actorUserID {
		return errNotFound
	}
	if cred.RevokedAt != nil {
		return nil
	}

	active, err := g.passkeys.ListActiveForUser(ctx, actorUserID)
	if err != nil {
		return err
	}
	others := 0
	for _, c := range active {
		if c.ID != credentialID {
			others++
		}
	}
	hasPassword, err := g.passwords.HasActive(ctx, actorUserID)
	if err != nil {
		return err
	}
	if others == 0 && !hasPassword {
		return errLastCredential
	}
	if err := g.passkeys.Revoke(ctx, credentialID); err != nil {
		return err
	}
	return g.sessions.RevokeAllForUser(ctx, actorUserID)
}
