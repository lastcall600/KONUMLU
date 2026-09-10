package identity

import (
	"context"
	"errors"
	"time"
)

// Passkeys is durable passkey credential persistence (no WebAuthn ceremony).
type Passkeys struct {
	store passkeyStore
	now   func() time.Time
}

func NewPasskeys(store passkeyStore, now func() time.Time) (*Passkeys, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Passkeys{store: store, now: now}, nil
}

func (p *Passkeys) Create(ctx context.Context, credential PasskeyCredential) error {
	if err := credential.Validate(); err != nil {
		return err
	}
	if credential.RevokedAt != nil {
		return errInvalidPasskey
	}
	return mapPasskeyStoreErr(p.store.InsertPasskey(ctx, credential))
}

func (p *Passkeys) FindActiveByCredentialID(ctx context.Context, credentialID []byte) (PasskeyCredential, error) {
	if len(credentialID) == 0 {
		return PasskeyCredential{}, errInvalidPasskey
	}
	c, err := p.store.GetActivePasskeyByCredentialID(ctx, credentialID)
	if err != nil {
		return PasskeyCredential{}, mapPasskeyStoreErr(err)
	}
	if !c.Active() {
		return PasskeyCredential{}, errNotFound
	}
	return c, nil
}

func (p *Passkeys) GetByCredentialID(ctx context.Context, credentialID []byte) (PasskeyCredential, error) {
	if len(credentialID) == 0 {
		return PasskeyCredential{}, errInvalidPasskey
	}
	c, err := p.store.GetPasskeyByCredentialID(ctx, credentialID)
	if err != nil {
		return PasskeyCredential{}, mapPasskeyStoreErr(err)
	}
	return c, nil
}

func (p *Passkeys) ListActiveForUser(ctx context.Context, userID ID) ([]PasskeyCredential, error) {
	if userID.IsZero() {
		return nil, errZeroID
	}
	list, err := p.store.ListActivePasskeysForUser(ctx, userID)
	if err != nil {
		return nil, mapPasskeyStoreErr(err)
	}
	out := make([]PasskeyCredential, 0, len(list))
	for _, c := range list {
		if c.Active() {
			out = append(out, c)
		}
	}
	return out, nil
}

func (p *Passkeys) RecordSuccessfulUse(ctx context.Context, id ID, signCount int64, backupState bool) error {
	if id.IsZero() {
		return errZeroID
	}
	c, err := p.store.GetPasskey(ctx, id)
	if err != nil {
		return mapPasskeyStoreErr(err)
	}
	next, err := c.ApplySuccessfulUse(signCount, backupState, p.now())
	if err != nil {
		return err
	}
	err = p.store.UpdatePasskeySuccessfulUse(ctx, next.ID, next.SignCount, next.BackupState, *next.LastUsedAt)
	if err != nil {
		return mapPasskeyStoreErr(err)
	}
	return nil
}

func (p *Passkeys) Revoke(ctx context.Context, id ID) error {
	if id.IsZero() {
		return errZeroID
	}
	return mapPasskeyStoreErr(p.store.RevokePasskey(ctx, id, p.now()))
}

func mapPasskeyStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errUnavailable) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidPasskey) || errors.Is(err, errCredentialRevoked) || errors.Is(err, errSignCountNotMonotonic) ||
		errors.Is(err, errCredentialConflict) {
		return err
	}
	return errUnavailable
}
