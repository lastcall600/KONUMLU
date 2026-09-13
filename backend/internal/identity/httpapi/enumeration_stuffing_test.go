package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"backend/internal/identity"
)

func TestAntiEnumerationPasswordKnownUnknownSameHTTP(t *testing.T) {
	unknown := newTestHandler(t)
	unknown.identifiers.err = identity.ErrUnauthenticated
	recUnknown := doPasswordLogin(t, unknown, "email", "nobody@example.com", "pw")

	known := newTestHandler(t)
	known.identifiers.user = identity.User{ID: mustID(t)}
	known.passwords.verifyErr = identity.ErrUnauthenticated
	recKnown := doPasswordLogin(t, known, "email", "owner@example.com", "pw")

	assertGenericAuthFailure(t, recUnknown, unknown)
	assertGenericAuthFailure(t, recKnown, known)
	if recUnknown.Code != recKnown.Code {
		t.Fatalf("status unknown=%d known=%d", recUnknown.Code, recKnown.Code)
	}
	if recUnknown.Body.String() != recKnown.Body.String() {
		t.Fatalf("body unknown=%s known=%s", recUnknown.Body.String(), recKnown.Body.String())
	}
	if recUnknown.Header().Get("Content-Type") != recKnown.Header().Get("Content-Type") {
		t.Fatal("content-type diverged")
	}
	if recUnknown.Header().Get("Retry-After") != recKnown.Header().Get("Retry-After") {
		t.Fatal("retry-after diverged")
	}
}

func TestAntiEnumerationSignupStartDoesNotRevealConflict(t *testing.T) {
	h := newTestHandler(t)
	id := mustID(t)
	h.signup.start = identity.StartSignupVerificationResult{ChallengeID: id}
	a := do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "new@example.com", "locale": "en",
	}, nil)
	b := do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "taken@example.com", "locale": "en",
	}, nil)
	if a.Code != http.StatusOK || b.Code != http.StatusOK {
		t.Fatalf("status a=%d b=%d", a.Code, b.Code)
	}
	if a.Body.String() != b.Body.String() {
		t.Fatalf("signup start bodies diverged: %s vs %s", a.Body.String(), b.Body.String())
	}
}

func TestAntiEnumerationPasskeyBeginIsOriginGeneric(t *testing.T) {
	h := newTestHandler(t)
	h.auth.begin = identity.BeginAuthenticationResult{RawToken: "t", Assertion: &protocol.CredentialAssertion{}}
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "exist") {
		t.Fatalf("enumeration leak: %s", rec.Body.String())
	}
}

func TestCredentialStuffingAccountDimensionIgnoresIPChange(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 100, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 1, PasswordUserWindow: time.Minute,
		TargetMaxAttempts: 100, TargetWindow: time.Minute,
	})
	h.guard.ip = func(*http.Request) string { return "203.0.113.10" }
	user := identity.User{ID: mustID(t)}
	h.identifiers.user = user
	h.passwords.verifyErr = identity.ErrUnauthenticated
	first := doPasswordLogin(t, h, "email", "owner@example.com", "a")
	assertGenericAuthFailure(t, first, h)
	h.guard.ip = func(*http.Request) string { return "198.51.100.20" }
	second := doPasswordLogin(t, h, "email", "owner@example.com", "b")
	assertGenericRateLimited(t, second)
	if h.passwords.verifyCalls != 1 {
		t.Fatalf("account bucket must stop verify after first, verifyCalls=%d", h.passwords.verifyCalls)
	}
}

func TestCredentialStuffingIPDimensionCoversManyAccounts(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 1, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 100, PasswordUserWindow: time.Minute,
	})
	h.identifiers.err = identity.ErrUnauthenticated
	first := doPasswordLogin(t, h, "email", "a@example.com", "x")
	assertGenericAuthFailure(t, first, h)
	second := doPasswordLogin(t, h, "email", "b@example.com", "x")
	assertGenericRateLimited(t, second)
	if h.identifiers.calls != 1 {
		t.Fatalf("second must 429 before resolve, calls=%d", h.identifiers.calls)
	}
}

func TestUnknownIdentifiersDoNotBypassProtection(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 20, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 1, PasswordUserWindow: time.Minute,
	})
	h.identifiers.err = identity.ErrUnauthenticated
	first := doPasswordLogin(t, h, "email", "ghost@example.com", "x")
	assertGenericAuthFailure(t, first, h)
	second := doPasswordLogin(t, h, "email", "ghost@example.com", "x")
	assertGenericRateLimited(t, second)
}

func TestPasskeyBeginFinishAbuseProtection(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 1, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 10, PasswordUserWindow: time.Minute,
	})
	h.auth.begin = identity.BeginAuthenticationResult{RawToken: "t", Assertion: &protocol.CredentialAssertion{}}
	first := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first = %d", first.Code)
	}
	second := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	assertGenericRateLimited(t, second)
}

func TestPasswordFallbackDummyVerifyOnUnknown(t *testing.T) {
	h := newTestHandler(t)
	h.identifiers.err = identity.ErrUnauthenticated
	rec := doPasswordLogin(t, h, "email", "nobody@example.com", "pw")
	assertGenericAuthFailure(t, rec, h)
	if h.passwords.dummyCalls != 1 {
		t.Fatalf("dummyCalls = %d", h.passwords.dummyCalls)
	}
	if h.passwords.verifyCalls != 0 {
		t.Fatal("unknown must not verify a real credential")
	}
}
