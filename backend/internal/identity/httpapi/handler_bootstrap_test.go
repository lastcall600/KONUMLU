package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/identity"
)

func TestPasswordlessSignupGrantsFirstPasskeyBootstrap(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	sessionID := mustID(t)
	h.accounts.result = identity.CompleteSignupResult{UserID: userID}
	h.sessions.issued = identity.IssuedSession{
		RawToken: "session-raw-token",
		Session:  identity.Session{ID: sessionID, UserID: userID},
	}
	h.sessions.resolved = h.sessions.issued.Session
	h.registration.begin = validRegisterBegin()

	rec := do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "one-time-signup-proof",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup = %d %s", rec.Code, rec.Body.String())
	}
	if h.bootstrap.grantSignupCalls != 1 {
		t.Fatalf("signup bootstrap grants = %d", h.bootstrap.grantSignupCalls)
	}
	if h.stepUp.grants != 0 {
		t.Fatal("signup must not grant step-up")
	}

	rec = doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("first passkey begin = %d %s", rec.Code, rec.Body.String())
	}
	if h.registration.begins != 1 {
		t.Fatal("bootstrap must allow first passkey begin")
	}
}

func TestPasswordSignupDoesNotGrantSignupBootstrap(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.accounts.result = identity.CompleteSignupResult{UserID: userID}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{ID: mustID(t), UserID: userID}}
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "one-time-signup-proof",
		"password":    "fallback-secret",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if h.bootstrap.grantSignupCalls != 0 {
		t.Fatal("password signup must use password re-auth, not signup bootstrap")
	}
}

func TestPasswordReauthThenFirstPasskeyBegin(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.stepUp.requireErr = identity.ErrStepUpRequired
	h.registration.begin = validRegisterBegin()

	rec := doPasswordReauth(t, h, "fallback-password-ok")
	if rec.Code != http.StatusOK {
		t.Fatalf("reauth = %d %s", rec.Code, rec.Body.String())
	}
	if h.bootstrap.grantPasswordCalls != 1 {
		t.Fatal("must grant password bootstrap")
	}
	if h.stepUp.grants != 0 {
		t.Fatal("password re-auth must not grant step-up")
	}
	rec = doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("begin = %d %s", rec.Code, rec.Body.String())
	}
}

func TestPasswordReauthWrongPasswordGeneric401(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.stepUp.requireErr = identity.ErrStepUpRequired
	h.bootstrap.grantPasswordErr = identity.ErrUnauthenticated
	h.registration.begin = validRegisterBegin()
	rec := doPasswordReauth(t, h, "wrong-password")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unauthenticated")
	if strings.Contains(strings.ToLower(rec.Body.String()), "wrong") || strings.Contains(rec.Body.String(), "wrong-password") {
		t.Fatal("must not leak password")
	}
	rec = doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("begin after failed reauth = %d", rec.Code)
	}
}

func TestPasswordReauthIsRateLimited(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts:           100,
		IPWindow:                time.Minute,
		PasswordUserMaxAttempts: 1,
		PasswordUserWindow:      time.Minute,
	})
	h.sessions.resolved = identity.Session{ID: mustID(t), UserID: mustID(t)}
	h.bootstrap.grantPasswordErr = identity.ErrUnauthenticated
	rec := doPasswordReauth(t, h, "wrong-password")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("first = %d", rec.Code)
	}
	rec = doPasswordReauth(t, h, "wrong-password")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second = %d %s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "rate_limited")
}

func TestPasswordBootstrapCannotAuthorizePasskeyRemoveHTTP(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.stepUp.requireErr = identity.ErrStepUpRequired
	if err := h.bootstrap.GrantPassword(nil, h.sessions.resolved, []byte("x")); err != nil {
		t.Fatal(err)
	}
	id := mustID(t)
	rec := do(t, h, http.MethodPost, "/v1/auth/passkeys/"+id.String()+"/remove", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	if len(h.credentials.removed) != 0 {
		t.Fatal("must not remove")
	}
}

func TestAnotherSessionCannotReuseSignupBootstrapHTTP(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	signupSession := identity.Session{ID: mustID(t), UserID: userID}
	h.accounts.result = identity.CompleteSignupResult{UserID: userID}
	h.sessions.issued = identity.IssuedSession{RawToken: "signup-session", Session: signupSession}
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "one-time-signup-proof",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup = %d", rec.Code)
	}
	h.sessions.resolved = identity.Session{ID: mustID(t), UserID: userID}
	h.registration.begin = validRegisterBegin()
	rec = doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other session begin = %d", rec.Code)
	}
}

func TestRevokedSessionCannotUseBootstrapHTTP(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.stepUp.requireErr = identity.ErrStepUpRequired
	if err := h.bootstrap.GrantSignup(nil, h.sessions.resolved); err != nil {
		t.Fatal(err)
	}
	h.sessions.resolveErr = identity.ErrUnauthenticated
	rec := doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked = %d", rec.Code)
	}
	rec = doPasswordReauth(t, h, "fallback-password-ok")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reauth revoked = %d", rec.Code)
	}
}

func TestBootstrapReplayAfterFirstPasskeyFinishFails(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.stepUp.requireErr = identity.ErrStepUpRequired
	h.registration.begin = validRegisterBegin()
	if err := h.bootstrap.GrantSignup(nil, h.sessions.resolved); err != nil {
		t.Fatal(err)
	}
	rec := doRegisterFinish(t, h, "reg-ceremony-token", map[string]any{"type": "public-key"})
	if rec.Code != http.StatusOK {
		t.Fatalf("finish = %d %s", rec.Code, rec.Body.String())
	}
	if len(h.bootstrap.consumed) != 1 {
		t.Fatal("successful enroll must consume bootstrap")
	}
	rec = doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("replay begin = %d", rec.Code)
	}
}

func TestEstablishedAccountStillRequiresPasskeyStepUp(t *testing.T) {
	h := newTestHandler(t)
	h.sessions.resolved = identity.Session{ID: mustID(t), UserID: mustID(t)}
	h.registration.begin = validRegisterBegin()
	h.credentials.list = []identity.PasskeyCredential{{ID: mustID(t), UserID: h.sessions.resolved.UserID}}
	rec := doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	if h.registration.begins != 0 {
		t.Fatal("established account must require step-up")
	}
	h.bootstrap.grantPasswordErr = identity.ErrStepUpRequired
	rec = doPasswordReauth(t, h, "fallback-password-ok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("password bootstrap on established = %d", rec.Code)
	}
}

func TestClientCannotSubmitBootstrapFlag(t *testing.T) {
	h := newTestHandler(t)
	h.sessions.resolved = identity.Session{ID: mustID(t), UserID: mustID(t)}
	h.registration.begin = validRegisterBegin()
	rec := doRegisterBegin(t, h, map[string]any{
		"bootstrap": true, "step_up": true, "stepUp": true, "recentStrong": true,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	if h.registration.begins != 0 {
		t.Fatal("client flag must not grant bootstrap")
	}
}

func TestSignupBootstrapGrantFailureFailsClosed(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	sessionID := mustID(t)
	h.accounts.result = identity.CompleteSignupResult{UserID: userID}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{ID: sessionID, UserID: userID}}
	h.bootstrap.grantSignupErr = identity.ErrUnavailable
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/complete", allowedOrigin, map[string]any{
		"signupProof": "one-time-signup-proof",
	}, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	if cookieHeader(rec, sessionCookieName) != "" {
		t.Fatal("must not set session cookie when bootstrap grant fails")
	}
	if len(h.sessions.revoked) != 1 || h.sessions.revoked[0] != sessionID {
		t.Fatalf("revoked = %v", h.sessions.revoked)
	}
}

func TestPasswordLoginStillDoesNotGrantBootstrap(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.identifiers.user = identity.User{ID: userID}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{ID: mustID(t), UserID: userID}}
	rec := doPasswordLogin(t, h, "email", "owner@example.com", "correct-horse")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if h.bootstrap.grantSignupCalls != 0 || h.bootstrap.grantPasswordCalls != 0 || h.stepUp.grants != 0 {
		t.Fatal("password login must not grant bootstrap or step-up")
	}
}

func doPasswordReauth(t *testing.T, h *testHandler, password string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/v1/auth/passkey/register/password-reauth", allowedOrigin, map[string]any{
		"password": password,
	}, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
}
