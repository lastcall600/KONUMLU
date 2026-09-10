package identity

import (
	"context"
	"errors"
	"time"
)

// IssuedSignupProof is returned once at issuance. RawToken must not be stored.
type IssuedSignupProof struct {
	Proof    SignupProof
	RawToken string
}

// SignupProofs issues short-lived, single-use, hash-only signup proofs (no HTTP, no login).
type SignupProofs struct {
	store signupProofStore
	ttl   time.Duration
	now   func() time.Time
}

func NewSignupProofs(store signupProofStore, policy SignupProofPolicy, now func() time.Time) (*SignupProofs, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SignupProofs{store: store, ttl: policy.TTL, now: now}, nil
}

func (p *SignupProofs) Issue(ctx context.Context, ch VerificationChallenge) (IssuedSignupProof, error) {
	if p == nil || p.store == nil {
		return IssuedSignupProof{}, errStoreRequired
	}
	if ch.Purpose != ChallengeSignup {
		return IssuedSignupProof{}, errInvalidChallenge
	}
	if ch.ConsumedAt == nil {
		return IssuedSignupProof{}, errInvalidChallenge
	}
	if err := ch.Validate(); err != nil {
		return IssuedSignupProof{}, err
	}
	raw, hash, err := GenerateSessionToken()
	if err != nil {
		return IssuedSignupProof{}, errUnavailable
	}
	id, err := NewID()
	if err != nil {
		return IssuedSignupProof{}, errUnavailable
	}
	now := p.now()
	proof := SignupProof{
		ID:                   id,
		ChallengeID:          ch.ID,
		Kind:                 ch.Kind,
		DestinationCanonical: ch.DestinationCanonical,
		Purpose:              SignupProofSignup,
		TokenHash:            hash,
		CreatedAt:            now,
		ExpiresAt:            now.Add(p.ttl),
	}
	if err := proof.Validate(); err != nil {
		return IssuedSignupProof{}, err
	}
	if err := p.store.InsertSignupProof(ctx, proof); err != nil {
		return IssuedSignupProof{}, mapSignupProofStoreErr(err)
	}
	return IssuedSignupProof{Proof: proof, RawToken: raw}, nil
}

// Consume atomically marks a valid, unexpired, unconsumed signup-purpose proof used.
// The UPDATE must run on the caller transaction so a later account-write failure rolls it back.
func (p *SignupProofs) Consume(ctx context.Context, tx transaction, raw string) (SignupProof, error) {
	if p == nil || p.store == nil {
		return SignupProof{}, errStoreRequired
	}
	if tx == nil {
		return SignupProof{}, errStoreRequired
	}
	secret, err := decodeSessionToken(raw)
	if err != nil {
		return SignupProof{}, errInvalidSignupProof
	}
	hash, err := HashSessionSecret(secret)
	if err != nil {
		return SignupProof{}, errInvalidSignupProof
	}
	proof, err := p.store.ConsumeSignupProofByTokenHash(ctx, tx, hash, p.now())
	if err != nil {
		return SignupProof{}, mapSignupProofStoreErr(err)
	}
	if err := proof.Validate(); err != nil {
		return SignupProof{}, err
	}
	if proof.Purpose != SignupProofSignup || proof.ConsumedAt == nil {
		return SignupProof{}, errInvalidSignupProof
	}
	return proof, nil
}

func mapSignupProofStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errUnavailable) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidSignupProof) || errors.Is(err, errSignupProofExpired) ||
		errors.Is(err, errSignupProofConsumed) || errors.Is(err, errInvalidChallenge) {
		return err
	}
	return errUnavailable
}
