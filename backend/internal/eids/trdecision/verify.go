package trdecision

import (
	"crypto/ed25519"
	"errors"
	"time"
)

var (
	ErrExpired        = errors.New("tr decision expired")
	ErrStale          = errors.New("tr decision stale")
	ErrFutureIssuedAt = errors.New("tr decision issued_at in the future")
	ErrClockPolicy    = errors.New("tr decision clock policy invalid")
)

type TimePolicy struct {
	MaxClockSkew  time.Duration
	MaxIngressAge time.Duration
}

func DefaultTimePolicy() TimePolicy {
	return TimePolicy{
		MaxClockSkew:  2 * time.Minute,
		MaxIngressAge: 10 * time.Minute,
	}
}

func (p TimePolicy) Validate() error {
	if p.MaxClockSkew <= 0 || p.MaxIngressAge <= 0 {
		return ErrClockPolicy
	}
	return nil
}

type Verifier struct {
	ring     PublicRing
	audience string
	policy   TimePolicy
	now      func() time.Time
}

func NewVerifier(ring PublicRing, audience string, policy TimePolicy, now func() time.Time) (*Verifier, error) {
	if ring.Len() == 0 {
		return nil, ErrInvalidKey
	}
	if audience != AudienceGermanyV1 {
		return nil, ErrInvalidAudience
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Verifier{ring: ring, audience: audience, policy: policy, now: now}, nil
}

// Verify is fail-closed. It reconstructs SigningBytes from typed claims.
func (v *Verifier) Verify(env Envelope, now time.Time) (ClaimsV1, error) {
	if v == nil || v.ring.Len() == 0 {
		return ClaimsV1{}, ErrInvalidKey
	}
	if now.IsZero() {
		now = v.now().UTC()
	} else {
		now = now.UTC()
	}
	if err := env.Claims.Validate(); err != nil {
		return ClaimsV1{}, err
	}
	if env.Claims.Audience != v.audience {
		return ClaimsV1{}, ErrInvalidAudience
	}
	pub, ok := v.ring.keys[env.KeyID]
	if !ok {
		return ClaimsV1{}, ErrUnknownKey
	}
	msg, err := env.Claims.SigningBytes(env.KeyID)
	if err != nil {
		return ClaimsV1{}, err
	}
	sig := env.signatureB
	if len(sig) == 0 {
		return ClaimsV1{}, ErrInvalidSignature
	}
	if !ed25519.Verify(pub, msg, sig) {
		return ClaimsV1{}, ErrInvalidSignature
	}
	if err := checkTime(env.Claims, v.policy, now); err != nil {
		return ClaimsV1{}, err
	}
	return env.Claims, nil
}

func checkTime(c ClaimsV1, p TimePolicy, now time.Time) error {
	if err := p.Validate(); err != nil {
		return err
	}
	issued := c.IssuedAt.UTC()
	until := c.ValidUntil.UTC()
	if !until.After(issued) {
		return ErrInvalidTime
	}
	if issued.After(now.Add(p.MaxClockSkew)) {
		return ErrFutureIssuedAt
	}
	if now.After(issued.Add(p.MaxIngressAge).Add(p.MaxClockSkew)) {
		return ErrStale
	}
	// Ingress freshness is independent of business valid_until. A verification
	// may remain product-valid after the signed envelope is no longer accepted
	// for transport. If valid_until is already past at verify time, reject.
	if now.After(until) {
		return ErrExpired
	}
	return nil
}
