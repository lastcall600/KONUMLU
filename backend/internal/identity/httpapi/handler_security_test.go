package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"backend/internal/identity"
)

func TestListSessionsReturnsOwnActiveOnly(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	current := h.sessions.resolved
	other := identity.Session{ID: mustID(t), UserID: current.UserID, CreatedAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(), AbsoluteExpiresAt: time.Now().Add(time.Hour).UTC()}
	h.sessions.listed = []identity.Session{current, other}

	rec := do(t, h, http.MethodGet, "/v1/auth/sessions", "", nil, map[string]string{sessionCookieName: "session-raw-token"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body sessionListResponse
	decode(t, rec, &body)
	if len(body.Sessions) != 2 {
		t.Fatalf("sessions = %d", len(body.Sessions))
	}
	var sawCurrent bool
	for _, s := range body.Sessions {
		if s.ID == current.ID.String() && s.Current {
			sawCurrent = true
		}
		if s.ID == "" {
			t.Fatal("missing session id")
		}
	}
	if !sawCurrent {
		t.Fatal("current session must be marked")
	}
}

func TestListSessionsUnauthorized(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/auth/sessions", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRevokeForeignSessionIsNotFound(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.sessions.ownedErr = identity.ErrNotFound
	foreign := mustID(t)
	rec := do(t, h, http.MethodPost, "/v1/auth/sessions/"+foreign.String()+"/revoke", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
	if len(h.sessions.revoked) != 0 {
		t.Fatal("must not revoke")
	}
}

func TestRevokeCurrentSessionClearsCookies(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.sessions.owned = h.sessions.resolved
	rec := do(t, h, http.MethodPost, "/v1/auth/sessions/"+h.sessions.resolved.ID.String()+"/revoke", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if line := cookieHeader(rec, sessionCookieName); line == "" || !containsMaxAgeExpired(line) {
		t.Fatalf("current revoke must clear session cookie: %s", line)
	}
}

func TestRevokeOthersRequiresCSRF(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/auth/sessions/revoke-others", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestPasskeyRegisterRequiresRecentStrongAuth(t *testing.T) {
	h := newTestHandler(t)
	h.sessions.resolved = identity.Session{ID: mustID(t), UserID: mustID(t)}
	h.registration.begin = validRegisterBegin()
	rec := doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "forbidden")
	if h.registration.begins != 0 {
		t.Fatal("must not begin registration without step-up")
	}
}

func TestListPasskeysOmitsSecrets(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	last := time.Now().UTC()
	h.credentials.list = []identity.PasskeyCredential{{
		ID:           mustID(t),
		UserID:       h.sessions.resolved.UserID,
		CredentialID: []byte("raw-credential-id-secret"),
		PublicKey:    []byte("public-key-material"),
		CreatedAt:    time.Now().UTC(),
		LastUsedAt:   &last,
		Transports:   []string{"internal"},
	}}
	rec := do(t, h, http.MethodGet, "/v1/auth/passkeys", "", nil, map[string]string{sessionCookieName: "session-raw-token"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if containsAny(body, "raw-credential-id-secret", "public-key-material", "session-raw-token") {
		t.Fatalf("leaked: %s", body)
	}
}

func TestRemoveForeignPasskeyIsNotFound(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.credentials.removeErr = identity.ErrNotFound
	id := mustID(t)
	rec := do(t, h, http.MethodPost, "/v1/auth/passkeys/"+id.String()+"/remove", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
}

func TestRemovePasskeyRequiresStepUp(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.stepUp.requireErr = identity.ErrStepUpRequired
	id := mustID(t)
	rec := do(t, h, http.MethodPost, "/v1/auth/passkeys/"+id.String()+"/remove", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(h.credentials.removed) != 0 {
		t.Fatal("must not remove")
	}
}

func TestRemoveLastPasskeyConflict(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.credentials.removeErr = identity.ErrLastCredential
	id := mustID(t)
	rec := do(t, h, http.MethodPost, "/v1/auth/passkeys/"+id.String()+"/remove", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "conflict")
}

func TestStepUpBeginAndFinishRotatesSession(t *testing.T) {
	h := newAuthedSecurityHandler(t)
	h.auth.stepBegin = identity.BeginAuthenticationResult{
		RawToken:  "step-ceremony",
		Assertion: &protocol.CredentialAssertion{Response: protocol.PublicKeyCredentialRequestOptions{RelyingPartyID: "example.test"}},
	}
	rec := do(t, h, http.MethodPost, "/v1/auth/step-up/passkey/begin", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("begin status = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.issued = identity.IssuedSession{RawToken: "rotated-session", Session: identity.Session{ID: mustID(t), UserID: h.sessions.resolved.UserID}}
	rec = do(t, h, http.MethodPost, "/v1/auth/step-up/passkey/finish", allowedOrigin, map[string]any{
		"ceremonyToken": "step-ceremony",
		"credential":    map[string]any{"type": "public-key"},
	}, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("finish status = %d body=%s", rec.Code, rec.Body.String())
	}
	if h.auth.stepFinishes != 1 {
		t.Fatal("finish must verify assertion")
	}
	if h.sessions.creates != 1 {
		t.Fatalf("creates = %d", h.sessions.creates)
	}
	if h.stepUp.grants != 1 {
		t.Fatalf("grants = %d", h.stepUp.grants)
	}
	assertHostCookie(t, rec, sessionCookieName, "rotated-session", true)
}

func TestClientCannotSubmitStepUpFlag(t *testing.T) {
	h := newTestHandler(t)
	h.sessions.resolved = identity.Session{ID: mustID(t), UserID: mustID(t)}
	h.registration.begin = validRegisterBegin()
	rec := doRegisterBegin(t, h, map[string]any{"step_up": true, "stepUp": true, "recentStrong": true, "bootstrap": true})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	if h.registration.begins != 0 {
		t.Fatal("client flag must not grant step-up")
	}
}

func TestLoginRotatesPredecessorOnly(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	pred := identity.Session{ID: mustID(t), UserID: userID}
	h.sessions.resolved = pred
	h.auth.finishID = userID
	h.sessions.issued = identity.IssuedSession{RawToken: "fresh-session", Session: identity.Session{ID: mustID(t), UserID: userID}}
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/finish", allowedOrigin, map[string]any{
		"ceremonyToken": "c-token",
		"credential":    map[string]any{"type": "public-key"},
	}, map[string]string{sessionCookieName: "old-session"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(h.sessions.revoked) != 1 || h.sessions.revoked[0] != pred.ID {
		t.Fatalf("revoked = %v", h.sessions.revoked)
	}
	assertHostCookie(t, rec, sessionCookieName, "fresh-session", true)
}

func TestPasswordLoginDoesNotGrantStepUp(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.identifiers.user = identity.User{ID: userID}
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{ID: mustID(t), UserID: userID}}
	rec := doPasswordLogin(t, h, "email", "owner@example.com", "correct-horse")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if h.stepUp.grants != 0 {
		t.Fatalf("password login grants = %d", h.stepUp.grants)
	}
}

func TestPasskeyLoginAttemptsStepUpGrant(t *testing.T) {
	h := newTestHandler(t)
	userID := mustID(t)
	h.auth.finishID = userID
	h.sessions.issued = identity.IssuedSession{RawToken: "fresh-session", Session: identity.Session{ID: mustID(t), UserID: userID}}
	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/login/finish", allowedOrigin, map[string]any{
		"ceremonyToken": "c-token",
		"credential":    map[string]any{"type": "public-key"},
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if h.stepUp.grants != 1 {
		t.Fatalf("passkey login grants = %d", h.stepUp.grants)
	}
}

func newAuthedSecurityHandler(t *testing.T) *testHandler {
	t.Helper()
	h := newTestHandler(t)
	h.sessions.resolved = identity.Session{
		ID:                mustID(t),
		UserID:            mustID(t),
		CreatedAt:         time.Now().UTC(),
		LastSeenAt:        time.Now().UTC(),
		AbsoluteExpiresAt: time.Now().Add(time.Hour).UTC(),
	}
	h.stepUp.requireErr = nil
	return h
}

func containsMaxAgeExpired(line string) bool {
	return strings.Contains(line, "Max-Age=0") || strings.Contains(line, "Max-Age=-1")
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
