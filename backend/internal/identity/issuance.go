package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
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
	ip := strings.TrimSpace(clientIP)
	if ip == "" {
		return nil
	}
	return l.check(ctx, issuanceIPKeyPrefix+string(purpose)+":"+ip, l.p.IPMax, l.p.IPWindow)
}

func (l *IssuanceLimiter) check(ctx context.Context, key string, max int, window time.Duration) error {
	n, err := l.inc.Increment(ctx, key, window)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return errUnavailable
	}
	if n > int64(max) {
		return errChallengeThrottled
	}
	return nil
}

func destinationIssuanceKey(kind IdentifierKind, purpose ChallengePurpose, canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return issuanceDestKeyPrefix + string(purpose) + ":" + string(kind) + ":" + hex.EncodeToString(sum[:])
}
