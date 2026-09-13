package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOTPAndVerificationAbuseProof(t *testing.T) {
	ctx := context.Background()
	store := newMemChallengeStore()
	inc := &memIssuanceCounter{}
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	current := now
	limiter, err := NewIssuanceLimiter(inc, IssuanceLimitPolicy{
		DestinationMax: 1, DestinationWindow: time.Hour,
		IPMax: 2, IPWindow: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewChallenges(store, testChallengePolicy(), limiter, mustProtector(t), func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "owner@example.com", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "owner@example.com", "203.0.113.5"); !errors.Is(err, errChallengeThrottled) {
		t.Fatalf("per-target cap: %v", err)
	}
	if _, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "second@example.com", "192.0.2.10"); err != nil {
		t.Fatalf("different dest same IP should be allowed within IP cap: %v", err)
	}
	if _, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "third@example.com", "192.0.2.10"); !errors.Is(err, errChallengeThrottled) {
		t.Fatalf("per-IP cap: %v", err)
	}

	if _, err := svc.Verify(ctx, first.Challenge.ID, "not-the-token"); err == nil {
		t.Fatal("wrong code must fail")
	}
	if _, err := svc.Verify(ctx, first.Challenge.ID, first.RawSecret); err != nil {
		t.Fatalf("valid verify: %v", err)
	}
	if _, err := svc.Verify(ctx, first.Challenge.ID, first.RawSecret); !errors.Is(err, errChallengeConsumed) {
		t.Fatalf("replay: %v", err)
	}
	if store.rawSecrets[first.Challenge.ID] != "" {
		t.Fatal("raw secret must not be stored")
	}

	current = now.Add(11 * time.Minute)
	late, err := NewChallenges(store, testChallengePolicy(), limiter, mustProtector(t), func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	expired, err := late.Issue(ctx, IdentifierPhone, ChallengePasswordReset, "+15551234567", "198.51.100.9")
	if err != nil {
		t.Fatal(err)
	}
	current = current.Add(11 * time.Minute)
	if _, err := late.Verify(ctx, expired.Challenge.ID, expired.RawSecret); !errors.Is(err, errChallengeExpired) {
		t.Fatalf("expiry: %v", err)
	}

	for _, key := range inc.keys {
		if strings.Contains(key, "owner@example.com") || strings.Contains(key, "+15551234567") {
			t.Fatalf("limiter key leaked destination: %q", key)
		}
	}

	down, err := NewChallenges(newMemChallengeStore(), testChallengePolicy(), mustLimiter(t, &memIssuanceCounter{fail: true}), mustProtector(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := down.Issue(ctx, IdentifierEmail, ChallengeSignup, "pump@example.com", "10.0.0.1"); !errors.Is(err, errUnavailable) {
		t.Fatalf("valkey down must fail closed: %v", err)
	}
}
