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
	txns      transactor
	security  TxSecurityRecorder
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
	return g.revokePasskeyWithSecurity(ctx, actorUserID, credentialID, SecurityRecord{})
}

func (g *CredentialGuard) BindDurableSecurity(txns transactor, rec TxSecurityRecorder) {
	if g == nil {
		return
	}
	g.txns = txns
	g.security = rec
}

func (g *CredentialGuard) RemovePasskeyWithSecurity(ctx context.Context, actorUserID, credentialID ID, rec SecurityRecord) error {
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
	return g.revokePasskeyWithSecurity(ctx, actorUserID, credentialID, rec)
}

func (g *CredentialGuard) revokePasskeyWithSecurity(ctx context.Context, actorUserID, credentialID ID, rec SecurityRecord) error {
	now := g.passkeys.now()
	if store, ok := g.txns.(passkeyMutationStore); ok && g.security != nil && rec.Type != "" {
		tx, err := store.Begin(ctx)
		if err != nil {
			return mapStoreErr(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := store.RevokePasskeyTx(ctx, tx, credentialID, now); err != nil {
			return mapPasskeyStoreErr(err)
		}
		epoch, err := store.RevokeSessionsForUserTx(ctx, tx, actorUserID, now)
		if err != nil {
			return mapStoreErr(err)
		}
		if err := g.security.RecordOn(ctx, tx, rec); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return mapStoreErr(err)
		}
		g.sessions.ApplyUserEpoch(ctx, actorUserID, epoch)
		return nil
	}
	if err := g.passkeys.Revoke(ctx, credentialID); err != nil {
		return err
	}
	if err := g.sessions.RevokeAllForUser(ctx, actorUserID); err != nil {
		return err
	}
	if rec.Type == "" || g.security == nil {
		return nil
	}
	return g.security.RecordOn(ctx, nil, rec)
}
