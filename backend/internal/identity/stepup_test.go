package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/cache"
)

func TestStepUpGrantRequireAndSessionBinding(t *testing.T) {
	ctx := context.Background()
	hot := newMemHotCache()
	svc, err := NewStepUp(hot, StepUpPolicy{TTL: time.Minute}, func() time.Time {
		return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	session := stepUpSession(t)
	other := stepUpSession(t)
	other.UserID = session.UserID
	if err := svc.Grant(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := svc.Require(ctx, session, SensitivePasskeyAdd); err != nil {
		t.Fatal(err)
	}
	if err := svc.Require(ctx, other, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("other session: %v", err)
	}
	svc.Clear(ctx, session.ID)
	if err := svc.Require(ctx, session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("cleared: %v", err)
	}
}

func TestStepUpUnavailableFailsClosed(t *testing.T) {
	ctx := context.Background()
	hot := newMemHotCache()
	hot.unavailable = true
	svc, err := NewStepUp(hot, StepUpPolicy{TTL: time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	session := stepUpSession(t)
	if err := svc.Grant(ctx, session); !errors.Is(err, errUnavailable) {
		t.Fatalf("grant: %v", err)
	}
	hot.unavailable = false
	if err := svc.Grant(ctx, session); err != nil {
		t.Fatal(err)
	}
	hot.unavailable = true
	if err := svc.Require(ctx, session, SensitivePasskeyRemove); !errors.Is(err, errUnavailable) {
		t.Fatalf("require: %v", err)
	}
}

func TestStepUpMissIsRequiredNotGrant(t *testing.T) {
	ctx := context.Background()
	hot := newMemHotCache()
	svc, err := NewStepUp(hot, StepUpPolicy{TTL: time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	session := stepUpSession(t)
	if err := svc.Require(ctx, session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := hot.kv[stepUpKey(session.ID)]; ok {
		t.Fatal("miss must not create elevation")
	}
	_ = cache.ErrMiss
}

func TestStepUpPolicyBounds(t *testing.T) {
	if _, err := NewStepUp(newMemHotCache(), StepUpPolicy{TTL: 0}, nil); !errors.Is(err, errInvalidStepUpPolicy) {
		t.Fatalf("zero: %v", err)
	}
	if _, err := NewStepUp(newMemHotCache(), StepUpPolicy{TTL: MaxStepUpTTL + time.Second}, nil); !errors.Is(err, errInvalidStepUpPolicy) {
		t.Fatalf("over max: %v", err)
	}
}

func stepUpSession(t *testing.T) Session {
	t.Helper()
	now := time.Now().UTC()
	id := mustID(t)
	user := mustID(t)
	hash := make([]byte, TokenHashSize)
	copy(hash, id[:])
	return Session{
		ID:                id,
		UserID:            user,
		TokenHash:         hash,
		CreatedAt:         now,
		LastSeenAt:        now,
		IdleExpiresAt:     now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	}
}
