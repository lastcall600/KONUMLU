package identity

import (
	"context"
	"errors"
	"time"
)

// IssuedResetProof is returned once at issuance. RawToken must not be stored.
type IssuedResetProof struct {
	Proof    PasswordResetProof
	RawToken string
}

// ResetProofs issues short-lived, single-use, hash-only password-reset proofs (no HTTP, no login).
type ResetProofs struct {
	store resetProofStore
	ttl   time.Duration
	now   func() time.Time
}

func NewResetProofs(store resetProofStore, policy ResetProofPolicy, now func() time.Time) (*ResetProofs, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ResetProofs{store: store, ttl: policy.TTL, now: now}, nil
}

func (p *ResetProofs) Issue(ctx context.Context, ch VerificationChallenge, userID ID) (IssuedResetProof, error) {
	if p == nil || p.store == nil {
		return IssuedResetProof{}, errStoreRequired
	}
	if ch.Purpose != ChallengePasswordReset {
		return IssuedResetProof{}, errInvalidChallenge
	}
	if ch.ConsumedAt == nil {
		return IssuedResetProof{}, errInvalidChallenge
	}
	if err := ch.Validate(); err != nil {
		return IssuedResetProof{}, err
	}
	if userID.IsZero() {
		return IssuedResetProof{}, errZeroID
	}
	raw, hash, err := GenerateSessionToken()
	if err != nil {
		return IssuedResetProof{}, errUnavailable
	}
	id, err := NewID()
	if err != nil {
		return IssuedResetProof{}, errUnavailable
	}
	now := p.now()
	proof := PasswordResetProof{
		ID:          id,
		UserID:      userID,
		ChallengeID: ch.ID,
		Purpose:     PasswordResetProofPasswordReset,
		TokenHash:   hash,
		CreatedAt:   now,
		ExpiresAt:   now.Add(p.ttl),
	}
	if err := proof.Validate(); err != nil {
		return IssuedResetProof{}, err
	}
	if err := p.store.InsertResetProof(ctx, proof); err != nil {
		return IssuedResetProof{}, mapResetProofStoreErr(err)
	}
	return IssuedResetProof{Proof: proof, RawToken: raw}, nil
}

// Consume atomically marks a valid, unexpired, unconsumed password-reset proof used.
// The UPDATE must run on the caller transaction so a later write failure rolls it back.
func (p *ResetProofs) Consume(ctx context.Context, tx transaction, raw string) (PasswordResetProof, error) {
	if p == nil || p.store == nil {
		return PasswordResetProof{}, errStoreRequired
	}
	if tx == nil {
		return PasswordResetProof{}, errStoreRequired
	}
	secret, err := decodeSessionToken(raw)
	if err != nil {
		return PasswordResetProof{}, errInvalidResetProof
	}
	hash, err := HashSessionSecret(secret)
	if err != nil {
		return PasswordResetProof{}, errInvalidResetProof
	}
	proof, err := p.store.ConsumeResetProofByTokenHash(ctx, tx, hash, p.now())
	if err != nil {
		return PasswordResetProof{}, mapResetProofStoreErr(err)
	}
	if err := proof.Validate(); err != nil {
		return PasswordResetProof{}, err
	}
	if proof.Purpose != PasswordResetProofPasswordReset || proof.ConsumedAt == nil {
		return PasswordResetProof{}, errInvalidResetProof
	}
	return proof, nil
}

func mapResetProofStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errUnavailable) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidResetProof) || errors.Is(err, errResetProofExpired) ||
		errors.Is(err, errResetProofConsumed) || errors.Is(err, errInvalidChallenge) {
		return err
	}
	return errUnavailable
}
