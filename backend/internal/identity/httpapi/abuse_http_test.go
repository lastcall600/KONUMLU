package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"backend/internal/identity"
)

func TestSignupAndResetStartHaveHTTPIPLimits(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 1, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 10, PasswordUserWindow: time.Minute,
	})
	h.signup.start = identity.StartSignupVerificationResult{ChallengeID: mustID(t)}
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "a@example.com", "locale": "en",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup start first = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "b@example.com", "locale": "en",
	}, nil)
	assertGenericRateLimited(t, rec)
	if h.signup.starts != 1 {
		t.Fatalf("starts = %d", h.signup.starts)
	}

	h2 := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 1, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 10, PasswordUserWindow: time.Minute,
	})
	h2.reset.start = identity.StartPasswordResetResult{ChallengeID: mustID(t)}
	rec = do(t, h2, http.MethodPost, "/v1/auth/password/reset/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "a@example.com", "locale": "en",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset start first = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h2, http.MethodPost, "/v1/auth/password/reset/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "a@example.com", "locale": "en",
	}, nil)
	assertGenericRateLimited(t, rec)
	if h2.reset.starts != 1 {
		t.Fatalf("reset starts = %d", h2.reset.starts)
	}
}

func TestSignupCompleteIsRateLimited(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 20, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 20, PasswordUserWindow: time.Minute,
		CompleteMaxAttempts: 1, CompleteWindow: time.Minute,
	})
	h.accounts.result = identity.CompleteSignupResult{UserID: mustID(t)}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{UserID: h.accounts.result.UserID}}
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "one-time-signup-proof",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("first = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "one-time-signup-proof",
	}, nil)
	assertGenericRateLimited(t, rec)
	if h.accounts.completes != 1 {
		t.Fatalf("completes = %d", h.accounts.completes)
	}
	assertNoProofInKeys(t, h, "one-time-signup-proof")
}

func TestResetVerifyAndCompleteAreRateLimited(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 20, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 20, PasswordUserWindow: time.Minute,
		TargetMaxAttempts: 1, TargetWindow: time.Minute,
		CompleteMaxAttempts: 1, CompleteWindow: time.Minute,
	})
	cid := mustID(t)
	h.reset.verify = identity.VerifyPasswordResetResult{ResetProof: "one-time-reset-proof"}
	rec := do(t, h, http.MethodPost, "/v1/auth/password/reset/verify", allowedOrigin, map[string]any{
		"challengeId": cid.String(), "code": "123456",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify first = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/auth/password/reset/verify", allowedOrigin, map[string]any{
		"challengeId": cid.String(), "code": "123456",
	}, nil)
	assertGenericRateLimited(t, rec)
	if h.reset.verifies != 1 {
		t.Fatalf("verifies = %d", h.reset.verifies)
	}

	h2 := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 20, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 20, PasswordUserWindow: time.Minute,
		CompleteMaxAttempts: 1, CompleteWindow: time.Minute,
	})
	h2.reset.complete = identity.CompletePasswordResetResult{UserID: mustID(t), SessionEpoch: 1}
	rec = do(t, h2, http.MethodPost, "/v1/auth/password/reset/complete", allowedOrigin, map[string]any{
		"resetProof": "one-time-reset-proof", "newPassword": "replacement-secret",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete first = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h2, http.MethodPost, "/v1/auth/password/reset/complete", allowedOrigin, map[string]any{
		"resetProof": "one-time-reset-proof", "newPassword": "replacement-secret",
	}, nil)
	assertGenericRateLimited(t, rec)
	if h2.reset.completes != 1 {
		t.Fatalf("completes = %d", h2.reset.completes)
	}
	assertNoProofInKeys(t, h2, "one-time-reset-proof")
}

func TestPasskeyRegisterEnrollmentIsRateLimited(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 20, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 20, PasswordUserWindow: time.Minute,
		SensitiveMaxAttempts: 1, SensitiveWindow: time.Minute,
	})
	userID := mustID(t)
	h.sessions.resolved = identity.Session{ID: mustID(t), UserID: userID}
	h.registration.begin = identity.BeginRegistrationResult{
		RawToken: "t",
		Creation: &protocol.CredentialCreation{},
	}
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/register/begin", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("first = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/auth/passkey/register/begin", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	assertGenericRateLimited(t, rec)
	if h.registration.begins != 1 {
		t.Fatalf("begins = %d", h.registration.begins)
	}
}

func TestSignupFinishHasIndependentTargetDimension(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 20, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 20, PasswordUserWindow: time.Minute,
		TargetMaxAttempts: 1, TargetWindow: time.Minute,
	})
	a := mustID(t)
	b := mustID(t)
	h.signup.finish = identity.FinishSignupVerificationResult{Verified: true, SignupProof: "p"}
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/verification/finish", allowedOrigin, map[string]any{
		"challengeId": a.String(), "code": "111111",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a first = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/verification/finish", allowedOrigin, map[string]any{
		"challengeId": a.String(), "code": "111111",
	}, nil)
	assertGenericRateLimited(t, rec)
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/verification/finish", allowedOrigin, map[string]any{
		"challengeId": b.String(), "code": "111111",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("other target must be independent: %d %s", rec.Code, rec.Body.String())
	}
}

func TestChallengeRequiredCannotBypassByOmittingToken(t *testing.T) {
	h := newChallengedHandler(t, identity.AuthOpPasswordLogin, identity.FakeHumanChallenge{})
	h.identifiers.user = identity.User{ID: mustID(t)}
	rec := doPasswordLogin(t, h, "email", "owner@example.com", "secret")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "forbidden")
	assertNoSensitiveLeak(t, rec.Body.String())
	if h.identifiers.calls != 0 {
		t.Fatal("missing challenge must not resolve identifiers")
	}
}

func TestChallengeValidFakeAllowsPasswordLogin(t *testing.T) {
	h := newChallengedHandler(t, identity.AuthOpPasswordLogin, identity.FakeHumanChallenge{})
	userID := mustID(t)
	h.identifiers.user = identity.User{ID: userID}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{UserID: userID}}
	rec := do(t, h, http.MethodPost, "/v1/auth/password/login", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "owner@example.com", "password": "correct-horse",
		"challengeToken": "ok:password_login",
	}, nil)
	assertPasswordLoginSuccess(t, rec, h, userID)
}

func TestChallengeInvalidAndWrongActionAreForbidden(t *testing.T) {
	h := newChallengedHandler(t, identity.AuthOpPasswordLogin, identity.FakeHumanChallenge{})
	rec := do(t, h, http.MethodPost, "/v1/auth/password/login", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "owner@example.com", "password": "x",
		"challengeToken": "bad-token",
	}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("invalid = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/auth/password/login", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "owner@example.com", "password": "x",
		"challengeToken": "ok:signup_complete",
	}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong action = %d", rec.Code)
	}
	if h.identifiers.calls != 0 {
		t.Fatal("failed challenge must not resolve identifiers")
	}
}

func TestChallengeProviderUnavailableFailsClosed(t *testing.T) {
	h := newChallengedHandler(t, identity.AuthOpSignupComplete, identity.UnconfiguredHumanChallenge{})
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "one-time-signup-proof", "challengeToken": "ok:signup_complete",
	}, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	if h.accounts.completes != 0 {
		t.Fatal("provider outage must not complete signup")
	}
}

func TestChallengeTimeoutFailsClosed(t *testing.T) {
	h := newChallengedHandler(t, identity.AuthOpPasswordLogin, identity.FakeHumanChallenge{Err: http.ErrHandlerTimeout})
	rec := do(t, h, http.MethodPost, "/v1/auth/password/login", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "owner@example.com", "password": "x",
		"challengeToken": "ok:password_login",
	}, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
}

func TestChallengeHostnameMismatchForbidden(t *testing.T) {
	policy := generousLimit()
	policy.Challenge = identity.HumanChallengePolicy{
		Provider:  identity.HumanChallengeProviderFake,
		Required:  map[identity.AuthOperation]struct{}{identity.AuthOpPasswordLogin: {}},
		Hostname:  "other.example.test",
		ReplayTTL: time.Minute,
	}
	policy.Verifier = identity.FakeHumanChallenge{ExpectedHostname: "other.example.test"}
	h := newTestHandlerWithLimit(t, policy)
	rec := do(t, h, http.MethodPost, "/v1/auth/password/login", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "owner@example.com", "password": "x",
		"challengeToken": "ok:password_login",
	}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChallengeNotRequiredDoesNotNeedToken(t *testing.T) {
	h := newTestHandler(t)
	h.auth.begin = identity.BeginAuthenticationResult{
		RawToken:  "t",
		Assertion: &protocol.CredentialAssertion{},
	}
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRateLimitDoesNotRevealAccountExistence(t *testing.T) {
	policy := AuthRateLimit{
		IPMaxAttempts: 20, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 1, PasswordUserWindow: time.Minute,
	}
	hUnknown := newTestHandlerWithLimit(t, policy)
	hUnknown.identifiers.err = identity.ErrUnauthenticated
	firstUnknown := doPasswordLogin(t, hUnknown, "email", "nobody@example.com", "x")
	secondUnknown := doPasswordLogin(t, hUnknown, "email", "nobody@example.com", "x")

	hKnown := newTestHandlerWithLimit(t, policy)
	hKnown.identifiers.user = identity.User{ID: mustID(t)}
	hKnown.passwords.verifyErr = identity.ErrUnauthenticated
	firstKnown := doPasswordLogin(t, hKnown, "email", "owner@example.com", "x")
	secondKnown := doPasswordLogin(t, hKnown, "email", "owner@example.com", "x")

	assertGenericAuthFailure(t, firstUnknown, hUnknown)
	assertGenericAuthFailure(t, firstKnown, hKnown)
	if firstUnknown.Body.String() != firstKnown.Body.String() {
		t.Fatalf("first-attempt body diverged: %s vs %s", firstUnknown.Body.String(), firstKnown.Body.String())
	}
	assertGenericRateLimited(t, secondUnknown)
	assertGenericRateLimited(t, secondKnown)
	if secondUnknown.Body.String() != secondKnown.Body.String() {
		t.Fatalf("rate limit body diverged: %s vs %s", secondUnknown.Body.String(), secondKnown.Body.String())
	}
	if secondUnknown.Header().Get("Retry-After") != secondKnown.Header().Get("Retry-After") {
		t.Fatalf("Retry-After diverged: %q vs %q", secondUnknown.Header().Get("Retry-After"), secondKnown.Header().Get("Retry-After"))
	}
	if hUnknown.identifiers.calls != 1 {
		t.Fatalf("unknown second attempt must 429 before resolve, calls=%d", hUnknown.identifiers.calls)
	}
	if hKnown.identifiers.calls != 1 {
		t.Fatalf("known second attempt must 429 before resolve, calls=%d", hKnown.identifiers.calls)
	}
}

func TestChallengePolicyDoesNotDependOnAccountResolution(t *testing.T) {
	hUnknown := newChallengedHandler(t, identity.AuthOpPasswordLogin, identity.FakeHumanChallenge{})
	hUnknown.identifiers.err = identity.ErrUnauthenticated
	recUnknown := doPasswordLogin(t, hUnknown, "email", "nobody@example.com", "x")
	hKnown := newChallengedHandler(t, identity.AuthOpPasswordLogin, identity.FakeHumanChallenge{})
	hKnown.identifiers.user = identity.User{ID: mustID(t)}
	recKnown := doPasswordLogin(t, hKnown, "email", "owner@example.com", "x")
	if recUnknown.Code != http.StatusForbidden || recKnown.Code != http.StatusForbidden {
		t.Fatalf("status unknown=%d known=%d", recUnknown.Code, recKnown.Code)
	}
	assertErrorCode(t, recUnknown, "forbidden")
	assertErrorCode(t, recKnown, "forbidden")
	if recUnknown.Body.String() != recKnown.Body.String() {
		t.Fatalf("challenge body diverged: %s vs %s", recUnknown.Body.String(), recKnown.Body.String())
	}
	if hUnknown.identifiers.calls != 0 || hKnown.identifiers.calls != 0 {
		t.Fatal("challenge must run before identifier resolve")
	}
}

func newChallengedHandler(t *testing.T, op identity.AuthOperation, verifier identity.HumanChallenge) *testHandler {
	t.Helper()
	policy := generousLimit()
	policy.Challenge = identity.HumanChallengePolicy{
		Provider:  verifier.Name(),
		Required:  map[identity.AuthOperation]struct{}{op: {}},
		ReplayTTL: time.Minute,
	}
	policy.Verifier = verifier
	return newTestHandlerWithLimit(t, policy)
}

func assertNoProofInKeys(t *testing.T, h *testHandler, raw string) {
	t.Helper()
	for _, key := range h.counter.keys {
		if strings.Contains(key, raw) {
			t.Fatalf("raw proof in key %q", key)
		}
	}
}
