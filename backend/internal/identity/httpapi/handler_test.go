package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"backend/internal/identity"
)

const allowedOrigin = "https://app.example.test"

func TestBeginPasskeyLoginReturnsOptionsAndToken(t *testing.T) {
	h := newTestHandler(t)
	h.auth.begin = identity.BeginAuthenticationResult{
		RawToken:  "ceremony-raw-token",
		Assertion: &protocol.CredentialAssertion{Response: protocol.PublicKeyCredentialRequestOptions{RelyingPartyID: "example.test"}},
	}

	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body beginResponse
	decode(t, rec, &body)
	if body.CeremonyToken != "ceremony-raw-token" {
		t.Fatalf("ceremonyToken = %q", body.CeremonyToken)
	}
	if body.PublicKey.RelyingPartyID != "example.test" {
		t.Fatalf("publicKey.rpId = %q", body.PublicKey.RelyingPartyID)
	}
	if cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("begin must not set a session cookie")
	}
}

func TestFinishSuccessSetsSessionAndCSRFCookies(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.auth.finishID = userID
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{UserID: userID}}

	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/finish", allowedOrigin, map[string]any{
		"ceremonyToken": "c-token",
		"credential":    map[string]any{"type": "public-key"},
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if h.sessions.creates != 1 {
		t.Fatalf("creates = %d, want 1", h.sessions.creates)
	}
	assertHostCookie(t, rec, sessionCookieName, "session-raw-token", true)
	csrf := cookieByName(rec, csrfCookieName)
	if csrf == nil || csrf.Value == "" {
		t.Fatal("csrf cookie must be set")
	}
	assertHostCookie(t, rec, csrfCookieName, csrf.Value, false)
	if csrf.HttpOnly {
		t.Fatal("csrf cookie must not be HttpOnly")
	}

	var body authResponse
	decode(t, rec, &body)
	if !body.Authenticated || body.UserID != userID.String() {
		t.Fatalf("response = %+v", body)
	}
}

func TestFinishFailureCreatesNoSessionCookie(t *testing.T) {
	h := newTestHandler(t)
	h.auth.finishErr = identity.ErrUnauthenticated

	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/finish", allowedOrigin, map[string]any{
		"ceremonyToken": "c-token",
		"credential":    map[string]any{"type": "public-key"},
	}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	if h.sessions.creates != 0 {
		t.Fatal("failed finish must not create a session")
	}
	if cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("failed finish must not set a session cookie")
	}
	assertNoSensitiveLeak(t, rec.Body.String())
}

func TestFinishCounterConflictIsGenericAuthFailure(t *testing.T) {
	h := newTestHandler(t)
	h.auth.finishErr = identity.ErrCounterConflict

	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/finish", allowedOrigin, map[string]any{
		"ceremonyToken": "c-token",
		"credential":    map[string]any{"type": "public-key"},
	}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")
	assertNoSensitiveLeak(t, rec.Body.String())
	if strings.Contains(strings.ToLower(rec.Body.String()), "counter") {
		t.Fatal("must not leak counter details")
	}
}

func TestFinishMalformedJSONIsBadRequest(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/passkey/login/finish", strings.NewReader("{"))
	req.Header.Set("Origin", allowedOrigin)
	rec := httptest.NewRecorder()
	h.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "bad_request")
}

func TestGetSessionValidAndInvalid(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.sessions.resolved = identity.Session{UserID: userID}

	rec := do(t, h, http.MethodGet, "/v1/auth/session", "", nil, map[string]string{sessionCookieName: "ok-token"})
	if rec.Code != http.StatusOK {
		t.Fatalf("valid status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body authResponse
	decode(t, rec, &body)
	if !body.Authenticated || body.UserID != userID.String() {
		t.Fatalf("response = %+v", body)
	}

	h.sessions.resolveErr = identity.ErrUnauthenticated
	rec = do(t, h, http.MethodGet, "/v1/auth/session", "", nil, map[string]string{sessionCookieName: "bad"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")
	assertNoSensitiveLeak(t, rec.Body.String())

	rec = do(t, h, http.MethodGet, "/v1/auth/session", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing cookie status = %d", rec.Code)
	}
}

func TestLogoutRevokesAndClearsCookies(t *testing.T) {
	h := newTestHandler(t)
	sessionID := mustID(t)
	h.sessions.resolved = identity.Session{ID: sessionID, UserID: mustID(t)}

	rec := do(t, h, http.MethodPost, "/v1/auth/logout", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(h.sessions.revoked) != 1 || h.sessions.revoked[0] != sessionID {
		t.Fatalf("revoked = %v", h.sessions.revoked)
	}
	assertClearedCookie(t, rec, sessionCookieName, true)
	assertClearedCookie(t, rec, csrfCookieName, false)
}

func TestLogoutIdempotentWithoutSession(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/auth/logout", allowedOrigin, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(h.sessions.revoked) != 0 {
		t.Fatal("no session must not call revoke")
	}
	assertClearedCookie(t, rec, sessionCookieName, true)
	assertClearedCookie(t, rec, csrfCookieName, false)
}

func TestLogoutMissingOrMismatchedCSRFRejected(t *testing.T) {
	h := newTestHandler(t)
	sessionID := mustID(t)
	h.sessions.resolved = identity.Session{ID: sessionID}

	rec := do(t, h, http.MethodPost, "/v1/auth/logout", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing header status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, "/v1/auth/logout", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("other-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatch status = %d", rec.Code)
	}
	if len(h.sessions.revoked) != 0 {
		t.Fatal("CSRF failure must not revoke")
	}
	assertErrorCode(t, rec, "forbidden")
}

func TestInvalidOriginRejected(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", "https://evil.example", nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")

	rec = do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", "", nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", "*", nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wildcard origin status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, "/v1/auth/password/login", "https://evil.example", map[string]any{
		"kind": "email", "identifier": "a@example.com", "password": "x",
	}, nil)
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", "https://evil.example", map[string]any{
		"kind": "email", "identifier": "a@example.com", "locale": "tr",
	}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("signup start origin status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")
	if h.signup.starts != 0 {
		t.Fatal("invalid origin must not start signup verification")
	}

	rec = do(t, h, http.MethodPost, "/v1/auth/signup/verification/finish", "https://evil.example", map[string]any{
		"challengeId": mustID(t).String(), "code": "123456",
	}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("signup finish origin status = %d", rec.Code)
	}
	if h.signup.finishes != 0 {
		t.Fatal("invalid origin must not finish signup verification")
	}

	rec = do(t, h, http.MethodPost, "/v1/auth/signup/complete", "https://evil.example", map[string]any{
		"signupProof": "raw-signup-proof",
	}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("signup complete origin status = %d", rec.Code)
	}
	if h.accounts.completes != 0 {
		t.Fatal("invalid origin must not complete signup")
	}
	if h.sessions.creates != 0 {
		t.Fatal("invalid origin must not issue a session")
	}

	rec = do(t, h, http.MethodPost, "/v1/auth/password/reset/start", "https://evil.example", map[string]any{
		"kind": "email", "identifier": "a@example.com", "locale": "tr",
	}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("reset start origin status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")
	if h.reset.starts != 0 {
		t.Fatal("invalid origin must not start password reset")
	}

	rec = do(t, h, http.MethodPost, "/v1/auth/password/reset/verify", "https://evil.example", map[string]any{
		"challengeId": mustID(t).String(), "code": "123456",
	}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("reset verify origin status = %d", rec.Code)
	}
	if h.reset.verifies != 0 {
		t.Fatal("invalid origin must not verify password reset")
	}

	rec = do(t, h, http.MethodPost, "/v1/auth/password/reset/complete", "https://evil.example", map[string]any{
		"resetProof": "raw-reset-proof", "newPassword": "x",
	}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("reset complete origin status = %d", rec.Code)
	}
	if h.reset.completes != 0 {
		t.Fatal("invalid origin must not complete password reset")
	}
	if h.sessions.creates != 0 {
		t.Fatal("invalid origin must not issue a session")
	}

	rec = do(t, h, http.MethodPost, "/v1/auth/passkey/register/begin", "https://evil.example", map[string]any{
		"name": "x",
	}, map[string]string{sessionCookieName: "tok", csrfCookieName: "csrf"}, withCSRF("csrf"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("register begin origin status = %d", rec.Code)
	}
	if h.registration.begins != 0 {
		t.Fatal("invalid origin must not begin passkey registration")
	}

	rec = do(t, h, http.MethodPost, "/v1/auth/passkey/register/finish", "https://evil.example", map[string]any{
		"ceremonyToken": "c-token",
		"credential":    map[string]any{"type": "public-key"},
	}, map[string]string{sessionCookieName: "tok", csrfCookieName: "csrf"}, withCSRF("csrf"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("register finish origin status = %d", rec.Code)
	}
	if h.registration.finishes != 0 {
		t.Fatal("invalid origin must not finish passkey registration")
	}
	if len(h.counter.keys) != 0 {
		t.Fatalf("invalid origin must not consume rate-limit keys: %v", h.counter.keys)
	}
}

func TestAllowedOriginAccepted(t *testing.T) {
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

func TestInfrastructureErrorMapsTo503(t *testing.T) {
	h := newTestHandler(t)
	h.auth.beginErr = identity.ErrUnavailable
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("begin status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())

	h.sessions.resolveErr = identity.ErrUnavailable
	rec = do(t, h, http.MethodGet, "/v1/auth/session", "", nil, map[string]string{sessionCookieName: "tok"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("session status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "session store unavailable") {
		t.Fatal("must not leak store details")
	}

	h.identifiers.err = identity.ErrUnavailable
	rec = doPasswordLogin(t, h, "email", "ready@example.com", "secret")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("password resolve status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
	if h.passwords.dummyCalls != 0 {
		t.Fatal("DB failure must not run dummy auth")
	}
	if h.sessions.creates != 0 {
		t.Fatal("DB failure must not create a session")
	}

	h.signup.startErr = identity.ErrUnavailable
	h.signup.start = identity.StartSignupVerificationResult{ChallengeID: mustID(t)}
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "ready@example.com", "locale": "tr",
	}, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("signup start infra status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
}

func TestSignupVerificationStartEmailAndPhone(t *testing.T) {
	h := newTestHandler(t)
	emailID := mustID(t)
	h.signup.start = identity.StartSignupVerificationResult{ChallengeID: emailID}
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "Owner@Example.com", "locale": "tr",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("email status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertSignupStartBody(t, rec, emailID)
	if h.signup.lastStart.Kind != identity.IdentifierEmail || h.signup.lastStart.Locale != "tr" {
		t.Fatalf("start input = %+v", h.signup.lastStart)
	}
	if cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("signup start must not set a session cookie")
	}

	phoneID := mustID(t)
	h.signup.start = identity.StartSignupVerificationResult{ChallengeID: phoneID}
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "phone", "identifier": "+15551234567", "locale": "en",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("phone status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertSignupStartBody(t, rec, phoneID)
	if h.signup.lastStart.Kind != identity.IdentifierPhone {
		t.Fatalf("phone kind = %s", h.signup.lastStart.Kind)
	}
}

func TestSignupVerificationStartInvalidLocale(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "a@example.com", "locale": "fr",
	}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "bad_request")
	if h.signup.starts != 0 {
		t.Fatal("invalid locale must not call domain start")
	}
}

func TestSignupVerificationStartDuplicateDoesNotLeakExistence(t *testing.T) {
	h := newTestHandler(t)
	id := mustID(t)
	h.signup.start = identity.StartSignupVerificationResult{ChallengeID: id}
	recNew := do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "fresh@example.com", "locale": "tr",
	}, nil)
	recExisting := do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "taken@example.com", "locale": "tr",
	}, nil)
	if recNew.Code != http.StatusOK || recExisting.Code != http.StatusOK {
		t.Fatalf("status new=%d existing=%d", recNew.Code, recExisting.Code)
	}
	assertSignupStartBody(t, recExisting, id)
	body := strings.ToLower(recExisting.Body.String())
	if strings.Contains(body, "exist") || strings.Contains(body, "registered") || strings.Contains(body, "conflict") {
		t.Fatalf("existence leak: %s", recExisting.Body.String())
	}
}

func TestSignupVerificationFinishIssuesProof(t *testing.T) {
	h := newTestHandler(t)
	challengeID := mustID(t)
	h.signup.finish = identity.FinishSignupVerificationResult{Verified: true, SignupProof: "one-time-signup-proof"}
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/verification/finish", allowedOrigin, map[string]any{
		"challengeId": challengeID.String(),
		"code":        "123456",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body signupFinishResponse
	decode(t, rec, &body)
	if !body.Verified || body.SignupProof != "one-time-signup-proof" {
		t.Fatalf("response = %+v", body)
	}
	if cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("signup finish must not issue a login session")
	}
	if h.signup.lastFinish.ChallengeID != challengeID {
		t.Fatal("challenge id must be forwarded")
	}
}

func TestSignupVerificationFinishGenericFailures(t *testing.T) {
	h := newTestHandler(t)
	challengeID := mustID(t)
	cases := []error{
		identity.ErrInvalidChallenge,
		identity.ErrChallengeExpired,
		identity.ErrChallengeConsumed,
		identity.ErrChallengeExhausted,
	}
	for _, ferr := range cases {
		h.signup.finishErr = ferr
		rec := do(t, h, http.MethodPost, "/v1/auth/signup/verification/finish", allowedOrigin, map[string]any{
			"challengeId": challengeID.String(),
			"code":        "000000",
		}, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%v status = %d", ferr, rec.Code)
		}
		assertErrorCode(t, rec, "unauthenticated")
		body := rec.Body.String()
		if strings.Contains(strings.ToLower(body), "expired") || strings.Contains(strings.ToLower(body), "consumed") {
			t.Fatalf("detail leak: %s", body)
		}
		if cookieHeader(rec, sessionCookieName) != "" {
			t.Fatal("failure must not set a session cookie")
		}
	}

	h.signup.finishErr = identity.ErrUnavailable
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/verification/finish", allowedOrigin, map[string]any{
		"challengeId": challengeID.String(),
		"code":        "000000",
	}, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("infra status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
}

func TestSignupCompleteSuccessIssuesSession(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.accounts.result = identity.CompleteSignupResult{UserID: userID}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{UserID: userID}}

	rec := do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "one-time-signup-proof",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if h.accounts.last.SignupProof != "one-time-signup-proof" {
		t.Fatalf("proof forwarded = %q", h.accounts.last.SignupProof)
	}
	if len(h.accounts.last.Password) != 0 {
		t.Fatal("omitted password must not be forwarded")
	}
	assertPasswordLoginSuccess(t, rec, h, userID)
}

func TestSignupCompleteWithPasswordForwardsSecretOnce(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.accounts.result = identity.CompleteSignupResult{UserID: userID}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{UserID: userID}}

	rec := do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "one-time-signup-proof",
		"password":    "fallback-secret",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if h.accounts.sawPassword != "fallback-secret" {
		t.Fatal("password must be forwarded to account creation")
	}
	body := rec.Body.String()
	if strings.Contains(body, "fallback-secret") || strings.Contains(body, "one-time-signup-proof") {
		t.Fatalf("response leaked secrets: %s", body)
	}
}

func TestSignupCompleteMalformedAndAuthFailures(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing proof status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "bad_request")
	if h.accounts.completes != 0 || h.sessions.creates != 0 {
		t.Fatal("malformed request must not create an account session")
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup/complete", strings.NewReader("{"))
	req.Header.Set("Origin", allowedOrigin)
	rec = httptest.NewRecorder()
	h.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed json status = %d", rec.Code)
	}

	h.accounts.err = identity.ErrSignupProofExpired
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "expired-proof-value",
	}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")
	if h.sessions.creates != 0 || cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("proof failure must not issue a session")
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "expired") {
		t.Fatal("must not leak proof expiry")
	}

	h.accounts.err = identity.ErrSignupProofConsumed
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "used-proof-value",
	}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("consumed status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")

	h.accounts.err = identity.ErrIdentifierConflict
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "dup-proof-value",
	}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "conflict")
	if cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("conflict must not set a session cookie")
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "dup-proof-value") {
		t.Fatal("must not echo the proof")
	}

	h.accounts.err = identity.ErrUnavailable
	rec = do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "any-proof",
	}, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("infra status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
	if h.sessions.creates != 0 {
		t.Fatal("infra failure must not issue a session")
	}
}

func TestPasswordResetStartKnownAndUnknownSameHTTP(t *testing.T) {
	h := newTestHandler(t)
	id := mustID(t)
	h.reset.start = identity.StartPasswordResetResult{ChallengeID: id}
	recKnown := do(t, h, http.MethodPost, "/v1/auth/password/reset/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "owner@example.com", "locale": "tr",
	}, nil)
	recUnknown := do(t, h, http.MethodPost, "/v1/auth/password/reset/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "nobody@example.com", "locale": "tr",
	}, nil)
	if recKnown.Code != http.StatusOK || recUnknown.Code != http.StatusOK {
		t.Fatalf("status known=%d unknown=%d", recKnown.Code, recUnknown.Code)
	}
	var known, unknown signupStartResponse
	decode(t, recKnown, &known)
	decode(t, recUnknown, &unknown)
	if known.ChallengeID != id.String() || unknown.ChallengeID != id.String() {
		t.Fatalf("bodies known=%+v unknown=%+v", known, unknown)
	}
	body := strings.ToLower(recUnknown.Body.String())
	if strings.Contains(body, "exist") || strings.Contains(body, "unknown") || strings.Contains(body, "not found") {
		t.Fatalf("enumeration leak: %s", recUnknown.Body.String())
	}
}

func TestPasswordResetVerifyIssuesProof(t *testing.T) {
	h := newTestHandler(t)
	challengeID := mustID(t)
	h.reset.verify = identity.VerifyPasswordResetResult{ResetProof: "one-time-reset-proof"}
	rec := do(t, h, http.MethodPost, "/v1/auth/password/reset/verify", allowedOrigin, map[string]any{
		"challengeId": challengeID.String(),
		"code":        "123456",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body resetVerifyResponse
	decode(t, rec, &body)
	if body.ResetProof != "one-time-reset-proof" {
		t.Fatalf("response = %+v", body)
	}
	if cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("reset verify must not issue a login session")
	}
}

func TestPasswordResetVerifyGenericFailures(t *testing.T) {
	h := newTestHandler(t)
	challengeID := mustID(t)
	for _, ferr := range []error{identity.ErrInvalidChallenge, identity.ErrChallengeExpired, identity.ErrChallengeConsumed} {
		h.reset.verifyErr = ferr
		rec := do(t, h, http.MethodPost, "/v1/auth/password/reset/verify", allowedOrigin, map[string]any{
			"challengeId": challengeID.String(), "code": "000000",
		}, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%v status = %d", ferr, rec.Code)
		}
		assertErrorCode(t, rec, "unauthenticated")
		assertNoSensitiveLeak(t, rec.Body.String())
	}
}

func TestPasswordResetCompleteDoesNotIssueSession(t *testing.T) {
	h := newTestHandler(t)
	h.reset.complete = identity.CompletePasswordResetResult{UserID: mustID(t), SessionEpoch: 1}
	rec := do(t, h, http.MethodPost, "/v1/auth/password/reset/complete", allowedOrigin, map[string]any{
		"resetProof":  "one-time-reset-proof",
		"newPassword": "replacement-secret",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body resetCompleteResponse
	decode(t, rec, &body)
	if !body.OK {
		t.Fatalf("response = %+v", body)
	}
	if h.sessions.creates != 0 || cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("password reset must not issue a login session")
	}
	if strings.Contains(rec.Body.String(), "replacement-secret") || strings.Contains(rec.Body.String(), "one-time-reset-proof") {
		t.Fatalf("response leaked secrets: %s", rec.Body.String())
	}
}

func TestSignupVerificationStartIssuanceLimited(t *testing.T) {
	h := newTestHandler(t)
	h.signup.startErr = identity.ErrChallengeThrottled
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "a@example.com", "locale": "en",
	}, nil)
	assertGenericRateLimited(t, rec)
}

func TestNoSensitiveErrorLeakage(t *testing.T) {
	h := newTestHandler(t)
	h.auth.finishErr = errors.New("webauthn verify: challenge mismatch sql: postgres argon2id $argon2id$v=19 hash")
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/finish", allowedOrigin, map[string]any{
		"ceremonyToken": "secret-ceremony-token-value",
		"credential":    map[string]any{"type": "public-key"},
	}, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret-ceremony-token-value") {
		t.Fatal("ceremony token leaked")
	}
	assertNoSensitiveLeak(t, body)

	h.identifiers.err = errors.New("identifier lookup sql: postgres user_identifiers password_hash argon2id")
	rec = doPasswordLogin(t, h, "email", "secret-user@example.com", "super-secret-password")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("password leak status = %d", rec.Code)
	}
	body = rec.Body.String()
	if strings.Contains(body, "secret-user@example.com") || strings.Contains(body, "super-secret-password") {
		t.Fatal("identifier or password leaked")
	}
	assertNoSensitiveLeak(t, body)
}

func TestPasswordLoginEmailSuccess(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.identifiers.user = identity.User{ID: userID}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{UserID: userID}}

	rec := doPasswordLogin(t, h, "email", "  Owner@Example.com ", "correct-horse")
	assertPasswordLoginSuccess(t, rec, h, userID)
	if h.identifiers.kind != identity.IdentifierEmail {
		t.Fatalf("kind = %q", h.identifiers.kind)
	}
}

func TestPasswordLoginPhoneSuccess(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.identifiers.user = identity.User{ID: userID}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{UserID: userID}}

	rec := doPasswordLogin(t, h, "phone", " +905551112233 ", "correct-horse")
	assertPasswordLoginSuccess(t, rec, h, userID)
	if h.identifiers.kind != identity.IdentifierPhone {
		t.Fatalf("kind = %q", h.identifiers.kind)
	}
}

func TestPasswordLoginWrongPasswordGeneric401(t *testing.T) {
	h := newTestHandler(t)
	h.identifiers.user = identity.User{ID: mustID(t)}
	h.passwords.verifyErr = identity.ErrUnauthenticated
	rec := doPasswordLogin(t, h, "email", "owner@example.com", "wrong")
	assertGenericAuthFailure(t, rec, h)
	if h.passwords.verifyCalls != 1 {
		t.Fatal("wrong password must verify the stored credential")
	}
	if h.passwords.dummyCalls != 0 {
		t.Fatal("resolved identifier must not use dummy PHC")
	}
}

func TestPasswordLoginUnknownIdentifierGeneric401(t *testing.T) {
	h := newTestHandler(t)
	h.identifiers.err = identity.ErrUnauthenticated
	rec := doPasswordLogin(t, h, "email", "nobody@example.com", "any-password")
	assertGenericAuthFailure(t, rec, h)
	if h.passwords.dummyCalls != 1 {
		t.Fatal("unknown identifier must run dummy Argon2id")
	}
	if h.passwords.verifyCalls != 0 {
		t.Fatal("unknown identifier must not verify a stored password")
	}
}

func TestPasswordLoginUnverifiedOrRevokedGeneric401(t *testing.T) {
	h := newTestHandler(t)
	h.identifiers.err = identity.ErrUnauthenticated
	rec := doPasswordLogin(t, h, "email", "pending@example.com", "any-password")
	assertGenericAuthFailure(t, rec, h)
	if h.passwords.dummyCalls != 1 {
		t.Fatal("unverified/revoked identifier must run dummy Argon2id")
	}
}

func TestPasswordLoginDisabledOrDeletedGeneric401(t *testing.T) {
	h := newTestHandler(t)
	h.identifiers.err = identity.ErrAccountIneligible
	rec := doPasswordLogin(t, h, "phone", "+905551112233", "any-password")
	assertGenericAuthFailure(t, rec, h)
	if h.passwords.dummyCalls != 1 {
		t.Fatal("disabled/deleted account must run dummy Argon2id")
	}
	if h.passwords.verifyCalls != 0 {
		t.Fatal("ineligible account must not verify a stored password")
	}
}

func TestPasswordLoginMissingPasswordGeneric401(t *testing.T) {
	h := newTestHandler(t)
	h.identifiers.user = identity.User{ID: mustID(t)}
	h.passwords.verifyErr = identity.ErrUnauthenticated
	rec := doPasswordLogin(t, h, "email", "owner@example.com", "present-but-unusable")
	assertGenericAuthFailure(t, rec, h)
}

func TestPasswordLoginMalformedRequest(t *testing.T) {
	h := newTestHandler(t)
	rec := doPasswordLogin(t, h, "username", "owner@example.com", "secret")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported kind status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "bad_request")

	rec = doPasswordLogin(t, h, "email", "not-an-email", "secret")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed identifier status = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/password/login", strings.NewReader("{"))
	req.Header.Set("Origin", allowedOrigin)
	rec = httptest.NewRecorder()
	h.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed json status = %d", rec.Code)
	}
}

func TestPasswordLoginVerifyUnavailableIs503(t *testing.T) {
	h := newTestHandler(t)
	h.identifiers.user = identity.User{ID: mustID(t)}
	h.passwords.verifyErr = identity.ErrUnavailable
	rec := doPasswordLogin(t, h, "email", "owner@example.com", "secret")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
	if h.sessions.creates != 0 {
		t.Fatal("unavailable verify must not create a session")
	}
	if cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("unavailable verify must not set a session cookie")
	}
}

func TestAuthIPLimitAllowsBelowThreshold(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 2, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 10, PasswordUserWindow: time.Minute,
	})
	h.auth.begin = identity.BeginAuthenticationResult{
		RawToken:  "t",
		Assertion: &protocol.CredentialAssertion{},
	}
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("first status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthIPLimitRejectsAboveThreshold(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 2, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 10, PasswordUserWindow: time.Minute,
	})
	h.auth.begin = identity.BeginAuthenticationResult{
		RawToken:  "t",
		Assertion: &protocol.CredentialAssertion{},
	}
	_ = do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	_ = do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertGenericRateLimited(t, rec)
	if h.auth.beginCalls != 2 {
		t.Fatalf("beginCalls = %d, want 2", h.auth.beginCalls)
	}
}

func TestPasswordUserLimitRejectsAboveThreshold(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 20, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 1, PasswordUserWindow: time.Minute,
	})
	userID := mustID(t)
	h.identifiers.user = identity.User{ID: userID}
	h.passwords.verifyErr = identity.ErrUnauthenticated

	rec := doPasswordLogin(t, h, "email", "owner@example.com", "wrong")
	assertGenericAuthFailure(t, rec, h)
	if h.passwords.verifyCalls != 1 {
		t.Fatal("first known-user attempt must verify password")
	}

	rec = doPasswordLogin(t, h, "email", "owner@example.com", "wrong")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertGenericRateLimited(t, rec)
	if h.passwords.verifyCalls != 1 {
		t.Fatal("user limit must run before password verification")
	}
	if h.sessions.creates != 0 {
		t.Fatal("user limit must not create a session")
	}
}

func TestUnknownIdentifierDoesNotCreatePIIKey(t *testing.T) {
	h := newTestHandler(t)
	h.identifiers.err = identity.ErrUnauthenticated
	email := "pii-user@example.com"
	phone := "+905551112233"
	_ = doPasswordLogin(t, h, "email", email, "any-password")
	_ = doPasswordLogin(t, h, "phone", phone, "any-password")
	for _, key := range h.counter.keys {
		if strings.Contains(key, email) || strings.Contains(strings.ToLower(key), "pii-user") ||
			strings.Contains(key, phone) || strings.Contains(key, "905551112233") {
			t.Fatalf("PII in limiter key %q", key)
		}
		if strings.Contains(key, authPasswordUserKeyPrefix) {
			t.Fatalf("unknown identifier must not create user bucket key %q", key)
		}
	}
	if h.passwords.dummyCalls != 2 {
		t.Fatalf("dummyCalls = %d, want 2", h.passwords.dummyCalls)
	}
}

func TestKnownUserLimitKeyUsesUUIDOnly(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.identifiers.user = identity.User{ID: userID}
	h.passwords.verifyErr = identity.ErrUnauthenticated
	email := "owner@example.com"
	rec := doPasswordLogin(t, h, "email", email, "wrong")
	assertGenericAuthFailure(t, rec, h)

	want := authPasswordUserKeyPrefix + userID.String()
	found := false
	for _, key := range h.counter.keys {
		if strings.Contains(key, email) {
			t.Fatalf("email in limiter key %q", key)
		}
		if key == want {
			found = true
		}
		if strings.HasPrefix(key, authPasswordUserKeyPrefix) && key != want {
			t.Fatalf("unexpected user key %q", key)
		}
	}
	if !found {
		t.Fatalf("missing user UUID key %q in %v", want, h.counter.keys)
	}
}

func TestRateLimitBackendUnavailableIs503(t *testing.T) {
	h := newTestHandler(t)
	h.counter.err = errors.New("dial tcp valkey")
	h.auth.begin = identity.BeginAuthenticationResult{
		RawToken:  "t",
		Assertion: &protocol.CredentialAssertion{},
	}
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("begin status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())
	if strings.Contains(rec.Body.String(), "valkey") || strings.Contains(rec.Body.String(), "dial") {
		t.Fatal("must not leak cache backend details")
	}
	if h.auth.beginCalls != 0 {
		t.Fatal("unavailable limiter must not continue passkey begin")
	}

	h.identifiers.user = identity.User{ID: mustID(t)}
	rec = doPasswordLogin(t, h, "email", "owner@example.com", "secret")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("password status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
	if h.identifiers.calls != 0 {
		t.Fatal("IP limiter failure must not resolve identifiers")
	}
	if h.passwords.verifyCalls != 0 || h.sessions.creates != 0 {
		t.Fatal("unavailable limiter must not authenticate")
	}
}

func TestRateLimitedResponseIsGeneric(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 1, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 10, PasswordUserWindow: time.Minute,
	})
	h.auth.begin = identity.BeginAuthenticationResult{
		RawToken:  "t",
		Assertion: &protocol.CredentialAssertion{},
	}
	_ = do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/begin", allowedOrigin, nil, nil)
	assertGenericRateLimited(t, rec)
	body := rec.Body.String()
	if strings.Contains(body, "192.0.2.1") || strings.Contains(body, authIPKeyPrefix) {
		t.Fatal("429 must not include IP or limiter keys")
	}
}

func TestNewRejectsWildcardOrigins(t *testing.T) {
	g := mustGuard(t, generousLimit(), &fakeCounter{})
	_, err := New(&fakeAuth{}, &fakeSessions{}, &fakeIdentifiers{}, &fakePasswords{}, &fakeSignup{}, &fakeAccounts{}, &fakeRegistration{}, &fakeReset{}, []string{"https://*.example.test"}, g)
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = New(&fakeAuth{}, &fakeSessions{}, &fakeIdentifiers{}, &fakePasswords{}, &fakeSignup{}, &fakeAccounts{}, &fakeRegistration{}, &fakeReset{}, nil, g)
	if err == nil {
		t.Fatal("expected error for empty allowlist")
	}
	_, err = New(&fakeAuth{}, &fakeSessions{}, &fakeIdentifiers{}, &fakePasswords{}, &fakeSignup{}, &fakeAccounts{}, &fakeRegistration{}, &fakeReset{}, []string{allowedOrigin}, nil)
	if err == nil {
		t.Fatal("expected error for nil abuse guard")
	}
	_, err = New(&fakeAuth{}, &fakeSessions{}, &fakeIdentifiers{}, &fakePasswords{}, nil, &fakeAccounts{}, &fakeRegistration{}, &fakeReset{}, []string{allowedOrigin}, g)
	if err == nil {
		t.Fatal("expected error for nil signup")
	}
	_, err = New(&fakeAuth{}, &fakeSessions{}, &fakeIdentifiers{}, &fakePasswords{}, &fakeSignup{}, nil, &fakeRegistration{}, &fakeReset{}, []string{allowedOrigin}, g)
	if err == nil {
		t.Fatal("expected error for nil accounts")
	}
	_, err = New(&fakeAuth{}, &fakeSessions{}, &fakeIdentifiers{}, &fakePasswords{}, &fakeSignup{}, &fakeAccounts{}, nil, &fakeReset{}, []string{allowedOrigin}, g)
	if err == nil {
		t.Fatal("expected error for nil passkey registrar")
	}
	_, err = New(&fakeAuth{}, &fakeSessions{}, &fakeIdentifiers{}, &fakePasswords{}, &fakeSignup{}, &fakeAccounts{}, &fakeRegistration{}, nil, []string{allowedOrigin}, g)
	if err == nil {
		t.Fatal("expected error for nil password reset")
	}
}

type fakeAuth struct {
	begin      identity.BeginAuthenticationResult
	beginErr   error
	beginCalls int
	finishID   identity.ID
	finishErr  error
}

func (f *fakeAuth) BeginAuthentication(context.Context) (identity.BeginAuthenticationResult, error) {
	f.beginCalls++
	return f.begin, f.beginErr
}

func (f *fakeAuth) FinishAuthentication(context.Context, identity.FinishAuthenticationInput) (identity.ID, error) {
	return f.finishID, f.finishErr
}

type fakeSessions struct {
	issued     identity.IssuedSession
	createErr  error
	creates    int
	resolved   identity.Session
	resolveErr error
	revoked    []identity.ID
	revokeErr  error
}

func (f *fakeSessions) Create(context.Context, identity.ID, *identity.ID) (identity.IssuedSession, error) {
	f.creates++
	return f.issued, f.createErr
}

func (f *fakeSessions) Resolve(context.Context, string) (identity.Session, error) {
	return f.resolved, f.resolveErr
}

func (f *fakeSessions) Revoke(_ context.Context, id identity.ID) error {
	f.revoked = append(f.revoked, id)
	return f.revokeErr
}

type fakeIdentifiers struct {
	user  identity.User
	err   error
	kind  identity.IdentifierKind
	raw   string
	calls int
}

func (f *fakeIdentifiers) ResolveVerified(_ context.Context, kind identity.IdentifierKind, raw string) (identity.User, error) {
	f.calls++
	f.kind = kind
	f.raw = raw
	return f.user, f.err
}

type fakePasswords struct {
	verifyErr   error
	verifyCalls int
	dummyCalls  int
}

func (f *fakePasswords) Verify(context.Context, identity.ID, []byte) (identity.PasswordVerifyResult, error) {
	f.verifyCalls++
	return identity.PasswordVerifyResult{}, f.verifyErr
}

func (f *fakePasswords) DummyVerify([]byte) {
	f.dummyCalls++
}

type fakeSignup struct {
	start    identity.StartSignupVerificationResult
	startErr error
	starts   int
	finish   identity.FinishSignupVerificationResult
	finishErr error
	finishes int
	lastStart identity.StartSignupVerificationInput
	lastFinish identity.FinishSignupVerificationInput
}

func (f *fakeSignup) StartSignupVerification(_ context.Context, in identity.StartSignupVerificationInput) (identity.StartSignupVerificationResult, error) {
	f.starts++
	f.lastStart = in
	return f.start, f.startErr
}

func (f *fakeSignup) FinishSignupVerification(_ context.Context, in identity.FinishSignupVerificationInput) (identity.FinishSignupVerificationResult, error) {
	f.finishes++
	f.lastFinish = in
	return f.finish, f.finishErr
}

type fakeAccounts struct {
	result      identity.CompleteSignupResult
	err         error
	completes   int
	last        identity.CompleteSignupInput
	sawPassword string
}

func (f *fakeAccounts) CompleteSignup(_ context.Context, in identity.CompleteSignupInput) (identity.CompleteSignupResult, error) {
	f.completes++
	f.last = in
	f.sawPassword = string(in.Password)
	return f.result, f.err
}

type fakeReset struct {
	start      identity.StartPasswordResetResult
	startErr   error
	starts     int
	verify     identity.VerifyPasswordResetResult
	verifyErr  error
	verifies   int
	complete   identity.CompletePasswordResetResult
	completeErr error
	completes  int
	lastStart  identity.StartPasswordResetInput
	lastVerify identity.VerifyPasswordResetInput
	lastComplete identity.CompletePasswordResetInput
}

func (f *fakeReset) StartPasswordReset(_ context.Context, in identity.StartPasswordResetInput) (identity.StartPasswordResetResult, error) {
	f.starts++
	f.lastStart = in
	return f.start, f.startErr
}

func (f *fakeReset) VerifyPasswordReset(_ context.Context, in identity.VerifyPasswordResetInput) (identity.VerifyPasswordResetResult, error) {
	f.verifies++
	f.lastVerify = in
	return f.verify, f.verifyErr
}

func (f *fakeReset) CompletePasswordReset(_ context.Context, in identity.CompletePasswordResetInput) (identity.CompletePasswordResetResult, error) {
	f.completes++
	f.lastComplete = in
	return f.complete, f.completeErr
}

type fakeRegistration struct {
	begin      identity.BeginRegistrationResult
	beginErr   error
	begins     int
	lastBegin  identity.BeginRegistrationInput
	finish     identity.PasskeyCredential
	finishErr  error
	finishes   int
	lastFinish identity.FinishRegistrationInput
}

func (f *fakeRegistration) BeginRegistration(_ context.Context, in identity.BeginRegistrationInput) (identity.BeginRegistrationResult, error) {
	f.begins++
	f.lastBegin = in
	return f.begin, f.beginErr
}

func (f *fakeRegistration) FinishRegistration(_ context.Context, in identity.FinishRegistrationInput) (identity.PasskeyCredential, error) {
	f.finishes++
	f.lastFinish = in
	return f.finish, f.finishErr
}

type fakeCounter struct {
	n    map[string]int64
	keys []string
	err  error
}

func (f *fakeCounter) Increment(_ context.Context, key string, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, errors.New("ttl must be greater than zero")
	}
	if f.err != nil {
		return 0, f.err
	}
	if f.n == nil {
		f.n = map[string]int64{}
	}
	f.keys = append(f.keys, key)
	f.n[key]++
	return f.n[key], nil
}

type testHandler struct {
	*Handler
	auth         *fakeAuth
	sessions     *fakeSessions
	identifiers  *fakeIdentifiers
	passwords    *fakePasswords
	signup       *fakeSignup
	accounts     *fakeAccounts
	registration *fakeRegistration
	reset        *fakeReset
	counter      *fakeCounter
}

func generousLimit() AuthRateLimit {
	return AuthRateLimit{
		IPMaxAttempts:           100,
		IPWindow:                time.Minute,
		PasswordUserMaxAttempts: 100,
		PasswordUserWindow:      time.Minute,
	}
}

func mustGuard(t *testing.T, policy AuthRateLimit, inc incrementer) *AbuseGuard {
	t.Helper()
	g, err := NewAbuseGuard(inc, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	return newTestHandlerWithLimit(t, generousLimit())
}

func newTestHandlerWithLimit(t *testing.T, policy AuthRateLimit) *testHandler {
	t.Helper()
	auth := &fakeAuth{}
	sessions := &fakeSessions{}
	identifiers := &fakeIdentifiers{}
	passwords := &fakePasswords{}
	signup := &fakeSignup{}
	accounts := &fakeAccounts{}
	registration := &fakeRegistration{}
	reset := &fakeReset{}
	counter := &fakeCounter{}
	h, err := New(auth, sessions, identifiers, passwords, signup, accounts, registration, reset, []string{allowedOrigin}, mustGuard(t, policy, counter))
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{
		Handler:      h,
		auth:         auth,
		sessions:     sessions,
		identifiers:  identifiers,
		passwords:    passwords,
		signup:       signup,
		accounts:     accounts,
		registration: registration,
		reset:        reset,
		counter:      counter,
	}
}

func (h *testHandler) mux() *http.ServeMux {
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func doPasswordLogin(t *testing.T, h *testHandler, kind, identifier, password string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/v1/auth/password/login", allowedOrigin, map[string]any{
		"kind":       kind,
		"identifier": identifier,
		"password":   password,
	}, nil)
}

func assertPasswordLoginSuccess(t *testing.T, rec *httptest.ResponseRecorder, h *testHandler, userID identity.ID) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if h.sessions.creates != 1 {
		t.Fatalf("creates = %d, want 1", h.sessions.creates)
	}
	if h.passwords.dummyCalls != 0 {
		t.Fatal("success must not run dummy Argon2id")
	}
	assertHostCookie(t, rec, sessionCookieName, "session-raw-token", true)
	csrf := cookieByName(rec, csrfCookieName)
	if csrf == nil || csrf.Value == "" {
		t.Fatal("csrf cookie must be set")
	}
	assertHostCookie(t, rec, csrfCookieName, csrf.Value, false)
	var body authResponse
	decode(t, rec, &body)
	if !body.Authenticated || body.UserID != userID.String() {
		t.Fatalf("response = %+v", body)
	}
}

func assertGenericRateLimited(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "rate_limited")
	assertNoSensitiveLeak(t, rec.Body.String())
	if cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("rate limit must not set a session cookie")
	}
}

func assertGenericAuthFailure(t *testing.T, rec *httptest.ResponseRecorder, h *testHandler) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unauthenticated")
	if h.sessions.creates != 0 {
		t.Fatal("auth failure must not create a session")
	}
	if cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("auth failure must not set a session cookie")
	}
	assertNoSensitiveLeak(t, rec.Body.String())
}

func do(t *testing.T, h *testHandler, method, path, origin string, jsonBody any, cookies map[string]string, opts ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if jsonBody != nil {
		b, err := json.Marshal(jsonBody)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, body)
	if jsonBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for name, val := range cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}
	for _, opt := range opts {
		opt(req)
	}
	rec := httptest.NewRecorder()
	h.mux().ServeHTTP(rec, req)
	return rec
}

func withCSRF(token string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set(csrfHeaderName, token)
	}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dest); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body errorResponse
	decode(t, rec, &body)
	if body.Error != code {
		t.Fatalf("error = %q, want %q", body.Error, code)
	}
}

func cookieHeader(rec *httptest.ResponseRecorder, name string) string {
	for _, line := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(line, name+"=") {
			return line
		}
	}
	return ""
}

func cookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func assertHostCookie(t *testing.T, rec *httptest.ResponseRecorder, name, value string, httpOnly bool) {
	t.Helper()
	line := cookieHeader(rec, name)
	if line == "" {
		t.Fatalf("missing Set-Cookie for %s", name)
	}
	if !strings.Contains(line, name+"="+value) {
		t.Fatalf("cookie %s value missing in %q", name, line)
	}
	if !strings.Contains(line, "Path=/") {
		t.Fatalf("Path=/ missing: %q", line)
	}
	if !strings.Contains(line, "Secure") {
		t.Fatalf("Secure missing: %q", line)
	}
	if !strings.Contains(strings.ToLower(line), "samesite=lax") {
		t.Fatalf("SameSite=Lax missing: %q", line)
	}
	if strings.Contains(strings.ToLower(line), "domain=") {
		t.Fatalf("Domain must not be set: %q", line)
	}
	if strings.Contains(line, "Max-Age=") {
		t.Fatalf("browser session cookie must not set Max-Age: %q", line)
	}
	c := cookieByName(rec, name)
	if c == nil {
		t.Fatalf("cookie %s not parsed", name)
	}
	if c.HttpOnly != httpOnly {
		t.Fatalf("%s HttpOnly = %v, want %v", name, c.HttpOnly, httpOnly)
	}
}

func assertClearedCookie(t *testing.T, rec *httptest.ResponseRecorder, name string, httpOnly bool) {
	t.Helper()
	line := cookieHeader(rec, name)
	if line == "" {
		t.Fatalf("missing clear Set-Cookie for %s", name)
	}
	if !strings.Contains(line, "Max-Age=0") && !strings.Contains(line, "Max-Age=-1") {
		t.Fatalf("cleared cookie must expire: %q", line)
	}
	if !strings.Contains(line, "Path=/") || !strings.Contains(line, "Secure") {
		t.Fatalf("cleared cookie missing host attributes: %q", line)
	}
	if strings.Contains(strings.ToLower(line), "domain=") {
		t.Fatalf("Domain must not be set: %q", line)
	}
	c := cookieByName(rec, name)
	if c != nil && c.HttpOnly != httpOnly {
		t.Fatalf("%s HttpOnly = %v, want %v", name, c.HttpOnly, httpOnly)
	}
}

func assertSignupStartBody(t *testing.T, rec *httptest.ResponseRecorder, id identity.ID) {
	t.Helper()
	var body map[string]any
	decode(t, rec, &body)
	if len(body) != 1 {
		t.Fatalf("signup start keys = %#v", body)
	}
	got, _ := body["challengeId"].(string)
	if got != id.String() {
		t.Fatalf("challengeId = %q, want %q", got, id.String())
	}
	raw := strings.ToLower(rec.Body.String())
	for _, w := range []string{"otp", "token", "secret", "code", "exists", "registered"} {
		if strings.Contains(raw, w) {
			t.Fatalf("start response leaked %q: %s", w, rec.Body.String())
		}
	}
}

func assertNoSensitiveLeak(t *testing.T, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	banned := []string{
		"webauthn", "challenge", "postgres", "sql:", "argon2", "password",
		"session store", "database", "stack", "panic",
	}
	for _, w := range banned {
		if strings.Contains(lower, w) {
			t.Fatalf("sensitive token %q leaked in %q", w, body)
		}
	}
}

func mustID(t *testing.T) identity.ID {
	t.Helper()
	id, err := identity.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
