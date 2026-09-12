package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

const (
	issuanceDestKeyPrefix = "identity:verify:issue:dest:"
	issuanceIPKeyPrefix   = "identity:verify:issue:ip:"
)

// issuanceCounter is the Valkey atomic counter used for disposable issuance windows.
type issuanceCounter interface {
	Increment(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// IssuanceLimiter is a Valkey-backed SMS-pumping / resend throttle.
// Destination buckets hash the canonical address; they never contain raw email/phone.
type IssuanceLimiter struct {
	inc issuanceCounter
	p   IssuanceLimitPolicy
}

func NewIssuanceLimiter(inc issuanceCounter, policy IssuanceLimitPolicy) (*IssuanceLimiter, error) {
	if inc == nil {
		return nil, errStoreRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &IssuanceLimiter{inc: inc, p: policy}, nil
}

func (l *IssuanceLimiter) Allow(ctx context.Context, kind IdentifierKind, purpose ChallengePurpose, canonicalDestination, clientIP string) error {
	if l == nil || l.inc == nil {
		return errUnavailable
	}
	if !kind.valid() || !purpose.valid() || canonicalDestination == "" {
		return errInvalidChallenge
	}
	destKey := destinationIssuanceKey(kind, purpose, canonicalDestination)
	if err := l.check(ctx, destKey, l.p.DestinationMax, l.p.DestinationWindow); err != nil {
		return err
	}
	ipKey := issuanceIPKeyPrefix + string(purpose) + ":" + HashRateLimitSubject(subjectOrUnknown(clientIP))
	return l.check(ctx, ipKey, l.p.IPMax, l.p.IPWindow)
}

func (l *IssuanceLimiter) check(ctx context.Context, key string, max int, window time.Duration) error {
	err := checkRateLimit(ctx, l.inc, key, RateLimitBucket{Max: max, Window: window})
	if err == nil {
		return nil
	}
	if errors.Is(err, errRateLimited) {
		return errChallengeThrottled
	}
	return err
}

func destinationIssuanceKey(kind IdentifierKind, purpose ChallengePurpose, canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return issuanceDestKeyPrefix + string(purpose) + ":" + string(kind) + ":" + hex.EncodeToString(sum[:])
}
