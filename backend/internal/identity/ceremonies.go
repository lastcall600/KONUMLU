package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"
)

// IssuedCeremony is returned once at creation. RawToken must not be stored.
type IssuedCeremony struct {
	State    CeremonyState
	RawToken string
}

// CeremonyPolicy is injected ceremony lifetime (ADR-004 OI-004-04 duration).
// TTL is not a product default; callers must supply a positive value.
type CeremonyPolicy struct {
	TTL time.Duration
}

func (p CeremonyPolicy) Validate() error {
	if p.TTL <= 0 {
		return errInvalidCeremonyPolicy
	}
	return nil
}

// Ceremonies is single-use WebAuthn ceremony-state lifecycle (PostgreSQL; no Valkey, no JWT).
type Ceremonies struct {
	store  ceremonyStore
	policy CeremonyPolicy
	now    func() time.Time
}

func NewCeremonies(store ceremonyStore, policy CeremonyPolicy, now func() time.Time) (*Ceremonies, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Ceremonies{store: store, policy: policy, now: now}, nil
}

func (c *Ceremonies) Create(ctx context.Context, kind CeremonyKind, userID *ID, sessionData []byte) (IssuedCeremony, error) {
	if !kind.valid() || len(sessionData) == 0 {
		return IssuedCeremony{}, errInvalidCeremony
	}
	if userID != nil && userID.IsZero() {
		return IssuedCeremony{}, errZeroID
	}

	raw, hash, err := GenerateCeremonyToken()
	if err != nil {
		return IssuedCeremony{}, errUnavailable
	}
	id, err := NewID()
	if err != nil {
		return IssuedCeremony{}, errUnavailable
	}

	now := c.now()
	state := CeremonyState{
		ID:          id,
		Kind:        kind,
		UserID:      userID,
		TokenHash:   hash,
		SessionData: cloneBytes(sessionData),
		CreatedAt:   now,
		ExpiresAt:   now.Add(c.policy.TTL),
	}
	if err := state.Validate(); err != nil {
		return IssuedCeremony{}, err
	}
	if err := c.store.InsertCeremony(ctx, state); err != nil {
		return IssuedCeremony{}, mapCeremonyStoreErr(err)
	}
	return IssuedCeremony{State: state, RawToken: raw}, nil
}

func (c *Ceremonies) Consume(ctx context.Context, rawToken string) (CeremonyState, error) {
	secret, err := decodeCeremonyToken(rawToken)
	if err != nil {
		return CeremonyState{}, errInvalidCeremony
	}
	hash, err := HashSessionSecret(secret)
	if err != nil {
		return CeremonyState{}, errInvalidCeremony
	}

	state, err := c.store.ConsumeCeremonyByTokenHash(ctx, hash, c.now())
	if err != nil {
		return CeremonyState{}, mapCeremonyStoreErr(err)
	}
	if subtle.ConstantTimeCompare(state.TokenHash, hash) != 1 {
		return CeremonyState{}, errInvalidCeremony
	}
	if err := state.Validate(); err != nil {
		return CeremonyState{}, err
	}
	if state.ConsumedAt == nil {
		return CeremonyState{}, errUnavailable
	}
	return state, nil
}

func mapCeremonyStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errUnavailable) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidCeremony) || errors.Is(err, errCeremonyExpired) || errors.Is(err, errCeremonyConsumed) {
		return err
	}
	return errUnavailable
}
