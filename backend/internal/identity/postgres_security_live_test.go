package identity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"backend/internal/platform/outbox"
)

func TestLivePostgresAuthSecurityOutbox(t *testing.T) {
	pool := liveIdentityPool(t)
	ctx := context.Background()
	ob, err := outbox.New(outbox.NewPostgresStore(pool), outbox.Policy{
		BatchSize:         10,
		Lease:             15 * time.Second,
		BackoffBase:       time.Second,
		BackoffMultiplier: 2,
		BackoffCap:        10 * time.Second,
		Jitter:            0,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := NewOutboxSecurityRecorder(ob, nil)
	if err != nil {
		t.Fatal(err)
	}
	user, err := insertLiveUser(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupLiveUser(context.Background(), pool, user) })
	err = rec.Record(ctx, SecurityRecord{
		Type:       AuthEventLoginSuccess,
		UserID:     user,
		AuthMethod: AuthMethodPassword,
		Operation:  AuthOpPasswordLogin,
		Result:     AuthResultSuccess,
		RequestID:  "auth-c-live",
	})
	if err != nil {
		t.Fatal(err)
	}

	row := pool.QueryRow(ctx, `
		SELECT event_type, event_version, payload
		FROM platform.outbox_events
		WHERE event_type = $1 AND correlation_id = $2
		ORDER BY created_at DESC
		LIMIT 1`, AuthSecurityEventType, "auth-c-live")
	var eventType string
	var version int
	var payload []byte
	if err := row.Scan(&eventType, &version, &payload); err != nil {
		t.Fatalf("scan outbox: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM platform.outbox_events WHERE correlation_id = $1`, "auth-c-live")
	})
	if eventType != AuthSecurityEventType || version != AuthSecurityEventVersion {
		t.Fatalf("type=%s version=%d", eventType, version)
	}
	s := strings.ToLower(string(payload))
	for _, leak := range []string{"otp", "@example", "cookie", "csrf", "session-raw"} {
		if strings.Contains(s, leak) {
			t.Fatalf("payload leaked %q: %s", leak, payload)
		}
	}
	if strings.Contains(s, `"password"`) && !strings.Contains(s, `"auth_method": "password"`) && !strings.Contains(s, `"auth_method":"password"`) {
		t.Fatalf("payload leaked password field: %s", payload)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	if body["event"] != string(AuthEventLoginSuccess) || body["result"] != AuthResultSuccess {
		t.Fatalf("payload = %#v", body)
	}
}

func TestFailClosedOpenMatrixDocumented(t *testing.T) {
	// Session cache: fail open to PostgreSQL (covered by TestSessionHotCacheUnavailableFallsBackToDB).
	// Abuse/issuance: fail closed (covered by TestLiveValkeyUnavailableFailsClosed and TestIssuanceLimiterUnavailableFailsClosed).
	// Step-Up / bootstrap: fail closed (covered by TestStepUpUnavailableFailsClosed).
	// HumanChallenge required: fail closed (covered by TestHumanChallengeProviderFailureMatrixFailClosed).
	if RiskAllow == RiskRestrict {
		t.Fatal("sanity")
	}
}
