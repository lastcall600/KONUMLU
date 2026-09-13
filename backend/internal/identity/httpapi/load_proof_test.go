package httpapi

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"golang.org/x/crypto/argon2"

	"backend/internal/identity"
)

// Provisional engineering targets for local AUTH-C load proof. These are not product SLOs.
const (
	authLoadProvisionalP99 = 250 * time.Millisecond
	authLoadConcurrency    = 32
	authLoadRequests       = 256
)

type loadStats struct {
	n         int
	ok        int
	errors    int
	limited   int
	latencies []time.Duration
}

func (s loadStats) percentile(p float64) time.Duration {
	if len(s.latencies) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), s.latencies...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(float64(len(cp)-1) * p)
	return cp[idx]
}

func TestAuthLoadProofDoesNotDisableSecurity(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 50, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 50, PasswordUserWindow: time.Minute,
	})
	h.identifiers.user = identity.User{ID: mustID(t)}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{UserID: h.identifiers.user.ID}}
	h.auth.begin = identity.BeginAuthenticationResult{RawToken: "t", Assertion: &protocol.CredentialAssertion{}}
	h.signup.start = identity.StartSignupVerificationResult{ChallengeID: mustID(t)}
	h.reset.start = identity.StartPasswordResetResult{ChallengeID: mustID(t)}
	h.sessions.resolved = identity.Session{ID: mustID(t), UserID: h.identifiers.user.ID}

	paths := []struct {
		name   string
		method string
		path   string
		body   map[string]any
		cookie bool
	}{
		{"password_login", http.MethodPost, "/v1/auth/password/login", map[string]any{"kind": "email", "identifier": "load@example.com", "password": "x"}, false},
		{"passkey_begin", http.MethodPost, "/v1/auth/passkey/login/begin", nil, false},
		{"signup_start", http.MethodPost, "/v1/auth/signup/verification/start", map[string]any{"kind": "email", "identifier": "load2@example.com", "locale": "en"}, false},
		{"reset_start", http.MethodPost, "/v1/auth/password/reset/start", map[string]any{"kind": "email", "identifier": "load3@example.com", "locale": "en"}, false},
		{"get_session", http.MethodGet, "/v1/auth/session", nil, true},
	}

	for _, p := range paths {
		stats := runLoad(t, h, p.method, p.path, allowedOrigin, p.body, p.cookie, authLoadConcurrency, authLoadRequests)
		p50 := stats.percentile(0.50)
		p95 := stats.percentile(0.95)
		p99 := stats.percentile(0.99)
		errRate := float64(stats.errors) / float64(stats.n)
		t.Logf("AUTH-C load %s concurrency=%d n=%d p50=%s p95=%s p99=%s error_rate=%.4f rate_limited=%d (provisional p99<%s, not a product SLO)",
			p.name, authLoadConcurrency, stats.n, p50, p95, p99, errRate, stats.limited, authLoadProvisionalP99)
		if stats.n != authLoadRequests {
			t.Fatalf("%s n=%d", p.name, stats.n)
		}
		if p99 > authLoadProvisionalP99 {
			t.Logf("provisional p99 exceeded for %s: %s (not a product SLO; recorded)", p.name, p99)
		}
	}

	limited := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 5, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 5, PasswordUserWindow: time.Minute,
	})
	limited.identifiers.err = identity.ErrUnauthenticated
	abuse := runLoad(t, limited, http.MethodPost, "/v1/auth/password/login", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "abuse@example.com", "password": "x",
	}, false, 16, 64)
	if abuse.limited == 0 {
		t.Fatal("abuse load must still return 429 after the limiter trips")
	}
	if limited.identifiers.calls > 10 {
		t.Fatalf("after limit, resolve should stop; calls=%d", limited.identifiers.calls)
	}
	t.Logf("AUTH-C abuse-load n=%d limited=%d errors=%d identifier_calls=%d", abuse.n, abuse.limited, abuse.errors, limited.identifiers.calls)
}

func TestAuthNormalFlowLoadProofKeepsSecurityEnabled(t *testing.T) {
	const (
		concurrency = 32
		total       = 256
	)
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 20, IPWindow: 15 * time.Minute,
		PasswordUserMaxAttempts: 10, PasswordUserWindow: 15 * time.Minute,
		TargetMaxAttempts: 10, TargetWindow: 15 * time.Minute,
		CompleteMaxAttempts: 10, CompleteWindow: 15 * time.Minute,
		SensitiveMaxAttempts: 10, SensitiveWindow: 15 * time.Minute,
	})
	h.guard.ip = ClientIPFromRemoteAddr
	ids := &varyingIdentifiers{users: map[string]identity.User{}}
	h.Handler.identifiers = ids
	h.identifiers = &fakeIdentifiers{}
	const loginPassword = "correct-horse"
	salt := []byte("load-proof-salt16")
	hash := argon2.IDKey([]byte(loginPassword), salt, 2, 19456, 1, 32)
	dummy := argon2.IDKey([]byte("dummy-verify-secret"), salt, 2, 19456, 1, 32)
	pw := &countingPasswords{salt: salt, hash: hash, dummy: dummy}
	h.Handler.passwords = pw
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{ID: mustID(t)}}
	h.auth.begin = identity.BeginAuthenticationResult{RawToken: "t", Assertion: &protocol.CredentialAssertion{}}
	h.signup.start = identity.StartSignupVerificationResult{ChallengeID: mustID(t)}
	h.reset.start = identity.StartPasswordResetResult{ChallengeID: mustID(t)}
	h.sessions.resolved = identity.Session{ID: mustID(t), UserID: mustID(t)}

	paths := []struct {
		name   string
		method string
		path   string
		cookie bool
		body   func(i int) map[string]any
	}{
		{"password_login", http.MethodPost, "/v1/auth/password/login", false, func(i int) map[string]any {
			return map[string]any{"kind": "email", "identifier": fmt.Sprintf("user%03d@example.test", i), "password": "correct-horse"}
		}},
		{"passkey_begin", http.MethodPost, "/v1/auth/passkey/login/begin", false, nil},
		{"signup_start", http.MethodPost, "/v1/auth/signup/verification/start", false, func(i int) map[string]any {
			return map[string]any{"kind": "email", "identifier": fmt.Sprintf("signup%03d@example.test", i), "locale": "en"}
		}},
		{"reset_start", http.MethodPost, "/v1/auth/password/reset/start", false, func(i int) map[string]any {
			return map[string]any{"kind": "email", "identifier": fmt.Sprintf("reset%03d@example.test", i), "locale": "en"}
		}},
		{"get_session", http.MethodGet, "/v1/auth/session", true, nil},
	}

	for _, p := range paths {
		stats := runNormalLoad(t, h, p.method, p.path, p.cookie, concurrency, total, p.body)
		p50 := stats.percentile(0.50)
		p95 := stats.percentile(0.95)
		p99 := stats.percentile(0.99)
		t.Logf("AUTH-C normal-flow %s concurrency=%d n=%d ok=%d limited=%d 5xx=%d p50=%s p95=%s p99=%s (provisional engineering, not a product SLO)",
			p.name, concurrency, stats.n, stats.ok, stats.limited, stats.errors, p50, p95, p99)
		if stats.n != total {
			t.Fatalf("%s n=%d", p.name, stats.n)
		}
		if p.name == "password_login" && pw.verifies.Load() < int64(total)*8/10 {
			t.Fatalf("password login must execute verification on normal requests; verifies=%d n=%d", pw.verifies.Load(), total)
		}
	}
}

func runNormalLoad(t *testing.T, h *testHandler, method, path string, withSession bool, concurrency, total int, bodyFn func(int) map[string]any) loadStats {
	t.Helper()
	var wg sync.WaitGroup
	var mu sync.Mutex
	stats := loadStats{}
	sem := make(chan struct{}, concurrency)
	var started atomic.Int64
	for started.Load() < int64(total) {
		i := int(started.Add(1) - 1)
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			cookies := map[string]string(nil)
			if withSession {
				cookies = map[string]string{sessionCookieName: "session-raw-token"}
			}
			var body map[string]any
			if bodyFn != nil {
				body = bodyFn(i)
			}
			start := time.Now()
			rec := do(t, h, method, path, allowedOrigin, body, cookies, withRemoteAddr(fmt.Sprintf("203.0.113.%d:1234", (i%250)+1)))
			d := time.Since(start)
			mu.Lock()
			stats.n++
			stats.latencies = append(stats.latencies, d)
			if rec.Code >= 200 && rec.Code < 300 {
				stats.ok++
			}
			if rec.Code >= 500 {
				stats.errors++
			}
			if rec.Code == http.StatusTooManyRequests {
				stats.limited++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return stats
}

func withRemoteAddr(addr string) func(*http.Request) {
	return func(r *http.Request) {
		r.RemoteAddr = addr
	}
}

type varyingIdentifiers struct {
	mu    sync.Mutex
	users map[string]identity.User
}

func (v *varyingIdentifiers) ResolveVerified(_ context.Context, _ identity.IdentifierKind, raw string) (identity.User, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if u, ok := v.users[raw]; ok {
		return u, nil
	}
	id, err := identity.NewID()
	if err != nil {
		return identity.User{}, err
	}
	u := identity.User{ID: id}
	if v.users == nil {
		v.users = map[string]identity.User{}
	}
	v.users[raw] = u
	return u, nil
}

type countingPasswords struct {
	verifies atomic.Int64
	salt     []byte
	hash     []byte
	dummy    []byte
}

func (c *countingPasswords) Verify(_ context.Context, _ identity.ID, password []byte) (identity.PasswordVerifyResult, error) {
	c.verifies.Add(1)
	got := argon2.IDKey(password, c.salt, 2, 19456, 1, 32)
	if subtle.ConstantTimeCompare(got, c.hash) != 1 {
		return identity.PasswordVerifyResult{}, identity.ErrUnauthenticated
	}
	return identity.PasswordVerifyResult{}, nil
}

func (c *countingPasswords) DummyVerify(password []byte) {
	_ = argon2.IDKey(password, c.salt, 2, 19456, 1, 32)
	_ = subtle.ConstantTimeCompare(c.dummy, c.dummy)
}

func runLoad(t *testing.T, h *testHandler, method, path, origin string, jsonBody any, withSession bool, concurrency, total int) loadStats {
	t.Helper()
	var wg sync.WaitGroup
	var mu sync.Mutex
	stats := loadStats{}
	sem := make(chan struct{}, concurrency)
	var started atomic.Int64
	for started.Load() < int64(total) {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			cookies := map[string]string(nil)
			if withSession {
				cookies = map[string]string{sessionCookieName: "session-raw-token"}
			}
			start := time.Now()
			rec := do(t, h, method, path, origin, jsonBody, cookies)
			d := time.Since(start)
			mu.Lock()
			stats.n++
			stats.latencies = append(stats.latencies, d)
			if rec.Code >= 500 {
				stats.errors++
			}
			if rec.Code == http.StatusTooManyRequests {
				stats.limited++
			}
			mu.Unlock()
		}()
		started.Add(1)
	}
	wg.Wait()
	return stats
}
