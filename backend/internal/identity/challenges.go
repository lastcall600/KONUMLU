package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"
)

// IssuedChallenge is returned once at issuance. RawSecret must not be stored.
type IssuedChallenge struct {
	Challenge VerificationChallenge
	RawSecret string
}

// Challenges is single-use email/phone verification-challenge lifecycle (no HTTP, no send).
type Challenges struct {
	store     challengeStore
	policy    VerificationChallengePolicy
	limiter   *IssuanceLimiter
	protector *MaterialProtector
	now       func() time.Time
}

func NewChallenges(store challengeStore, policy VerificationChallengePolicy, limiter *IssuanceLimiter, protector *MaterialProtector, now func() time.Time) (*Challenges, error) {
	if store == nil || protector == nil {
		return nil, errStoreRequired
	}
	if limiter != nil {
		if err := policy.Validate(); err != nil {
			return nil, err
		}
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Challenges{store: store, policy: policy, limiter: limiter, protector: protector, now: now}, nil
}

func (c *Challenges) Issue(ctx context.Context, kind IdentifierKind, purpose ChallengePurpose, rawDestination, clientIP string) (IssuedChallenge, error) {
	if c == nil || c.store == nil || c.protector == nil {
		return IssuedChallenge{}, errStoreRequired
	}
	if c.limiter == nil {
		return IssuedChallenge{}, errUnavailable
	}
	if !purpose.valid() {
		return IssuedChallenge{}, errInvalidChallenge
	}
	canonical, err := CanonicalizeIdentifier(kind, rawDestination)
	if err != nil {
		return IssuedChallenge{}, err
	}
	if err := c.limiter.Allow(ctx, kind, purpose, canonical, clientIP); err != nil {
		return IssuedChallenge{}, err
	}
	issued, sealed, err := c.prepareIssued(kind, purpose, canonical)
	if err != nil {
		return IssuedChallenge{}, err
	}
	if err := c.store.InsertChallengeAndMaterial(ctx, nil, issued.Challenge, sealed); err != nil {
		return IssuedChallenge{}, mapChallengeStoreErr(err)
	}
	return issued, nil
}

func (c *Challenges) prepareIssued(kind IdentifierKind, purpose ChallengePurpose, canonical string) (IssuedChallenge, VerificationMaterial, error) {
	raw, hash, err := generateChallengeSecret(kind, c.policy.PhoneOTPDigits)
	if err != nil {
		return IssuedChallenge{}, VerificationMaterial{}, errUnavailable
	}
	id, err := NewID()
	if err != nil {
		return IssuedChallenge{}, VerificationMaterial{}, errUnavailable
	}
	now := c.now()
	ch := VerificationChallenge{
		ID:                   id,
		Kind:                 kind,
		Purpose:              purpose,
		DestinationCanonical: canonical,
		TokenHash:            hash,
		CreatedAt:            now,
		ExpiresAt:            now.Add(c.policy.TTL),
		FailedAttempts:       0,
		MaxAttempts:          c.policy.MaxAttempts,
	}
	if err := ch.Validate(); err != nil {
		return IssuedChallenge{}, VerificationMaterial{}, err
	}
	sealed, err := c.protector.Seal(ch, []byte(raw))
	if err != nil {
		return IssuedChallenge{}, VerificationMaterial{}, mapChallengeStoreErr(err)
	}
	return IssuedChallenge{Challenge: ch, RawSecret: raw}, sealed, nil
}

// VerificationDelivery is transient plaintext for a trusted delivery caller.
// It must not be written to the database, outbox, or logs.
type VerificationDelivery struct {
	Kind                 IdentifierKind
	DestinationCanonical string
	Secret               string
}

func (d VerificationDelivery) String() string {
	return "identity.VerificationDelivery"
}

func (d VerificationDelivery) GoString() string {
	return "identity.VerificationDelivery{}"
}

// ResolveVerificationDelivery decrypts active material for a trusted delivery caller.
// Plaintext is transient. Outbox and Notifications storage must keep challenge_id only.
func (c *Challenges) ResolveVerificationDelivery(ctx context.Context, id ID) (VerificationDelivery, error) {
	if id.IsZero() {
		return VerificationDelivery{}, errZeroID
	}
	ch, err := c.store.GetChallenge(ctx, id)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return VerificationDelivery{}, errInvalidChallenge
		}
		return VerificationDelivery{}, mapChallengeStoreErr(err)
	}
	now := c.now()
	if err := ch.rejectUnusable(now); err != nil {
		_ = c.store.DestroyMaterial(ctx, id, now)
		return VerificationDelivery{}, err
	}
	mat, err := c.store.GetActiveMaterial(ctx, id)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return VerificationDelivery{}, errUnavailable
		}
		return VerificationDelivery{}, mapChallengeStoreErr(err)
	}
	plain, err := c.protector.Open(ch, mat)
	if err != nil {
		return VerificationDelivery{}, mapChallengeStoreErr(err)
	}
	secret := string(plain)
	if secret == "" {
		return VerificationDelivery{}, errUnavailable
	}
	return VerificationDelivery{
		Kind:                 ch.Kind,
		DestinationCanonical: ch.DestinationCanonical,
		Secret:               secret,
	}, nil
}

// ResolveDeliverySecret decrypts active material for a trusted delivery caller.
// Plaintext is transient. Outbox and Notifications storage must keep challenge_id only.
func (c *Challenges) ResolveDeliverySecret(ctx context.Context, id ID) (string, error) {
	got, err := c.ResolveVerificationDelivery(ctx, id)
	if err != nil {
		return "", err
	}
	return got.Secret, nil
}

func (c *Challenges) Verify(ctx context.Context, id ID, rawSecret string) (VerificationChallenge, error) {
	return c.verifySecret(ctx, id, rawSecret, nil)
}

func (c *Challenges) VerifyFor(ctx context.Context, id ID, rawSecret string, purpose ChallengePurpose) (VerificationChallenge, error) {
	return c.verifySecret(ctx, id, rawSecret, &purpose)
}

func (c *Challenges) verifySecret(ctx context.Context, id ID, rawSecret string, purpose *ChallengePurpose) (VerificationChallenge, error) {
	if id.IsZero() {
		return VerificationChallenge{}, errZeroID
	}
	if purpose != nil && !purpose.valid() {
		return VerificationChallenge{}, errInvalidChallenge
	}
	ch, err := c.store.GetChallenge(ctx, id)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return VerificationChallenge{}, errInvalidChallenge
		}
		return VerificationChallenge{}, mapChallengeStoreErr(err)
	}
	if purpose != nil && ch.Purpose != *purpose {
		return VerificationChallenge{}, errInvalidChallenge
	}
	now := c.now()
	if err := ch.rejectUnusable(now); err != nil {
		return VerificationChallenge{}, err
	}

	got, hashErr := hashSubmittedChallengeSecret(ch.Kind, rawSecret)
	match := 0
	if hashErr == nil {
		match = subtle.ConstantTimeCompare(ch.TokenHash, got)
	}
	if match == 1 {
		consumed, err := c.store.ConsumeChallenge(ctx, id, now)
		if err != nil {
			return VerificationChallenge{}, classifyChallengeMutationErr(err, ch, now)
		}
		if subtle.ConstantTimeCompare(consumed.TokenHash, got) != 1 {
			return VerificationChallenge{}, errInvalidChallenge
		}
		if consumed.ConsumedAt == nil {
			return VerificationChallenge{}, errUnavailable
		}
		if err := c.store.DestroyMaterial(ctx, id, now); err != nil && !errors.Is(err, errNotFound) {
			return VerificationChallenge{}, mapChallengeStoreErr(err)
		}
		return consumed, nil
	}

	n, err := c.store.IncrementFailedAttempt(ctx, id, now)
	if err != nil {
		return VerificationChallenge{}, classifyChallengeMutationErr(err, ch, now)
	}
	if n >= ch.MaxAttempts {
		return VerificationChallenge{}, errChallengeExhausted
	}
	return VerificationChallenge{}, errInvalidChallenge
}

func generateChallengeSecret(kind IdentifierKind, phoneDigits int) (string, []byte, error) {
	switch kind {
	case IdentifierEmail:
		return GenerateEmailVerificationToken()
	case IdentifierPhone:
		return GeneratePhoneOTP(phoneDigits)
	default:
		return "", nil, errInvalidChallenge
	}
}

func classifyChallengeMutationErr(err error, ch VerificationChallenge, now time.Time) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errNotFound) {
		if reject := ch.rejectUnusable(now); reject != nil {
			return reject
		}
		return errInvalidChallenge
	}
	return mapChallengeStoreErr(err)
}

func mapChallengeStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errUnavailable) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidChallenge) || errors.Is(err, errChallengeExpired) ||
		errors.Is(err, errChallengeConsumed) || errors.Is(err, errChallengeExhausted) ||
		errors.Is(err, errChallengeThrottled) || errors.Is(err, errInvalidIdentifier) {
		return err
	}
	return errUnavailable
}
