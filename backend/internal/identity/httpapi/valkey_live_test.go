package httpapi

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"backend/internal/identity"
	"backend/internal/platform/cache"
)

func liveValkey(t *testing.T) *cache.Client {
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

func TestLiveValkeyIndependentDimensionsExpireAndHash(t *testing.T) {
	c := liveValkey(t)
	ctx := context.Background()
	ns := "identity:auth:rl:test_live:"
	email := "live-synth@example.test"
	ip := "203.0.113.9"
	ipKey := ns + "ip:" + identity.HashRateLimitSubject(ip)
	tgtKey := ns + "target:" + identity.HashRateLimitSubject(email)
	t.Cleanup(func() {
		_ = c.Delete(context.Background(), ipKey)
		_ = c.Delete(context.Background(), tgtKey)
	})
	if strings.Contains(ipKey, ip) || strings.Contains(tgtKey, email) {
		t.Fatal("live keys must not contain raw identifiers")
	}
	n, err := c.Increment(ctx, ipKey, 2*time.Second)
	if err != nil || n != 1 {
		t.Fatalf("ip incr = %d %v", n, err)
	}
	n, err = c.Increment(ctx, tgtKey, 2*time.Second)
	if err != nil || n != 1 {
		t.Fatalf("target incr = %d %v", n, err)
	}
	n, err = c.Increment(ctx, ipKey, 2*time.Second)
	if err != nil || n != 2 {
		t.Fatalf("ip second = %d %v", n, err)
	}
	n, err = c.Increment(ctx, tgtKey, 2*time.Second)
	if err != nil || n != 2 {
		t.Fatalf("target must stay independent until its own increments: got %d", n)
	}
	c2, err := cache.Open(ctx, strings.TrimSpace(os.Getenv("VALKEY_URL")))
	if err != nil {
		c2, err = cache.Open(ctx, "redis://127.0.0.1:6379/0")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c2.Close)
	n, err = c2.Increment(ctx, ipKey, 2*time.Second)
	if err != nil || n != 3 {
		t.Fatalf("new client must see existing counter n=%d err=%v", n, err)
	}
	time.Sleep(2500 * time.Millisecond)
	n, err = c.Increment(ctx, ipKey, 2*time.Second)
	if err != nil || n != 1 {
		t.Fatalf("after TTL n=%d err=%v", n, err)
	}
}

func TestLiveValkeyUnavailableFailsClosed(t *testing.T) {
	ctx := context.Background()
	c, err := cache.Open(ctx, "redis://127.0.0.1:1/0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	policy, err := identity.BuildAuthAbusePolicy(
		identity.RateLimitBucket{Max: 5, Window: time.Minute},
		identity.RateLimitBucket{Max: 5, Window: time.Minute},
		identity.RateLimitBucket{Max: 5, Window: time.Minute},
		identity.RateLimitBucket{Max: 5, Window: time.Minute},
		identity.RateLimitBucket{Max: 5, Window: time.Minute},
	)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := identity.NewAbuseEngine(c, policy, identity.HumanChallengePolicy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := eng.Evaluate(ctx, identity.AbuseSubject{Operation: identity.AuthOpPasswordLogin, IP: "192.0.2.1"}, identity.PhaseIP)
	if out.Allow() || out.Reason != identity.ReasonStorageUnavailable {
		t.Fatalf("unreachable valkey must fail closed: %+v", out)
	}
}

func TestHTTPOverLiveValkeyThreshold(t *testing.T) {
	c := liveValkey(t)
	g, err := NewAbuseGuard(c, AuthRateLimit{
		IPMaxAttempts: 1, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 10, PasswordUserWindow: time.Minute,
	}, func(*http.Request) string { return "198.51.100.50" })
	if err != nil {
		t.Fatal(err)
	}
	auth := &fakeAuth{begin: identity.BeginAuthenticationResult{
		RawToken:  "t",
		Assertion: &protocol.CredentialAssertion{},
	}}
	h, err := New(auth, &fakeSessions{}, &fakeIdentifiers{}, &fakePasswords{}, &fakeSignup{}, &fakeAccounts{}, &fakeRegistration{}, &fakeReset{}, &fakeStepUp{}, &fakeCredentials{}, &fakeBootstrap{}, []string{allowedOrigin}, g)
	if err != nil {
		t.Fatal(err)
	}
	th := &testHandler{Handler: h, auth: auth, sessions: &fakeSessions{}, identifiers: &fakeIdentifiers{}, passwords: &fakePasswords{}, signup: &fakeSignup{}, accounts: &fakeAccounts{}, registration: &fakeRegistration{}, reset: &fakeReset{}, counter: &fakeCounter{}}
	rec := do(t, th, "POST", "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	if rec.Code != 200 {
		t.Fatalf("first = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, th, "POST", "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	assertGenericRateLimited(t, rec)
	ipKey := identity.FormatRateLimitKey(identity.AuthOpPasskeyLoginBegin, identity.DimensionIP, "198.51.100.50")
	t.Cleanup(func() { _ = c.Delete(context.Background(), ipKey) })
	if strings.Contains(ipKey, "198.51.100.50") {
		t.Fatal("raw IP in live key")
	}
}
