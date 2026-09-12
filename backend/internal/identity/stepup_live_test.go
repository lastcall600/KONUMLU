package identity

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"backend/internal/platform/cache"
)

func TestLiveValkeyStepUpSessionBoundAndFailClosed(t *testing.T) {
	c := liveValkeyClient(t)
	ctx := context.Background()
	svc, err := NewStepUp(c, StepUpPolicy{TTL: 2 * time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	session := stepUpSession(t)
	other := stepUpSession(t)
	other.UserID = session.UserID
	t.Cleanup(func() {
		_ = c.Delete(context.Background(), stepUpKey(session.ID))
		_ = c.Delete(context.Background(), stepUpKey(other.ID))
	})
	if err := svc.Grant(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := svc.Require(ctx, session, SensitivePasskeyAdd); err != nil {
		t.Fatal(err)
	}
	if err := svc.Require(ctx, other, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("other session = %v", err)
	}
	time.Sleep(2500 * time.Millisecond)
	if err := svc.Require(ctx, session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("expired = %v", err)
	}
	svc.Clear(ctx, session.ID)
	if err := svc.Require(ctx, session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("cleared = %v", err)
	}
}

func liveValkeyClient(t *testing.T) *cache.Client {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("VALKEY_URL"))
	if url == "" {
		url = "redis://127.0.0.1:6379/0"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := cache.Open(ctx, url)
	if err != nil {
		t.Skip("valkey not configured")
	}
	if err := c.Ping(ctx); err != nil {
		c.Close()
		t.Skip("local valkey not reachable")
	}
	t.Cleanup(c.Close)
	return c
}
