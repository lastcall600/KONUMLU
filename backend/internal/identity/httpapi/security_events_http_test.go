package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"backend/internal/identity"
)

func TestSecurityEventsCoverLoginLogoutRevokeAndAbuse(t *testing.T) {
	h := newTestHandler(t)
	sink := &identity.MemorySecurityRecorder{}
	h.SetSecurityRecorder(sink)
	user := identity.User{ID: mustID(t)}
	h.identifiers.user = user
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{ID: mustID(t), UserID: user.ID}}

	rec := doPasswordLogin(t, h, "email", "owner@example.com", "correct-password")
	assertPasswordLoginSuccess(t, rec, h, user.ID)

	h.sessions.resolved = h.sessions.issued.Session
	rec = do(t, h, http.MethodPost, "/v1/auth/logout", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("logout = %d %s", rec.Code, rec.Body.String())
	}

	types := sink.Types()
	if !containsEvent(types, identity.AuthEventLoginSuccess) || !containsEvent(types, identity.AuthEventLogout) {
		t.Fatalf("events = %v", types)
	}
	for _, ev := range sink.Snapshot() {
		raw, _ := json.Marshal(ev)
		s := strings.ToLower(string(raw))
		for _, leak := range []string{"correct-password", "owner@example.com", "session-raw-token", "csrf-token"} {
			if strings.Contains(s, strings.ToLower(leak)) {
				t.Fatalf("event leaked %q: %s", leak, raw)
			}
		}
	}
}

func TestUnknownLoginFailureEventOmitsUserAndIdentifier(t *testing.T) {
	h := newTestHandler(t)
	sink := &identity.MemorySecurityRecorder{}
	h.SetSecurityRecorder(sink)
	h.identifiers.err = identity.ErrUnauthenticated
	rec := doPasswordLogin(t, h, "email", "nobody@example.com", "secret-password")
	assertGenericAuthFailure(t, rec, h)
	found := false
	for _, ev := range sink.Snapshot() {
		if ev.Type != identity.AuthEventLoginFailed {
			continue
		}
		found = true
		if !ev.UserID.IsZero() {
			t.Fatal("unknown login must omit user id")
		}
		if ev.ReasonCode != identity.ReasonUnknownOrInvalid {
			t.Fatalf("reason = %s", ev.ReasonCode)
		}
	}
	if !found {
		t.Fatal("expected login.failed")
	}
}

func TestRateLimitAndChallengeEmitControlledEvents(t *testing.T) {
	h := newTestHandlerWithLimit(t, AuthRateLimit{
		IPMaxAttempts: 1, IPWindow: time.Minute,
		PasswordUserMaxAttempts: 10, PasswordUserWindow: time.Minute,
	})
	sink := &identity.MemorySecurityRecorder{}
	h.SetSecurityRecorder(sink)
	h.identifiers.err = identity.ErrUnauthenticated
	_ = doPasswordLogin(t, h, "email", "a@example.com", "x")
	rec := doPasswordLogin(t, h, "email", "b@example.com", "x")
	assertGenericRateLimited(t, rec)
	if !containsEvent(sink.Types(), identity.AuthEventRateLimitTriggered) {
		t.Fatalf("events = %v", sink.Types())
	}

	ch := generousLimit()
	ch.Challenge = identity.HumanChallengePolicy{
		Provider:  identity.HumanChallengeProviderFake,
		Required:  map[identity.AuthOperation]struct{}{identity.AuthOpPasswordLogin: {}},
		ReplayTTL: time.Minute,
	}
	ch.Verifier = identity.FakeHumanChallenge{}
	h2 := newTestHandlerWithLimit(t, ch)
	sink2 := &identity.MemorySecurityRecorder{}
	h2.SetSecurityRecorder(sink2)
	rec = doPasswordLogin(t, h2, "email", "owner@example.com", "x")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("challenge required status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "challenge_required")
	if !containsEvent(sink2.Types(), identity.AuthEventChallengeRequired) {
		t.Fatalf("events = %v", sink2.Types())
	}
}

func TestSignupStartDoesNotClaimDelivery(t *testing.T) {
	h := newTestHandler(t)
	h.signup.start = identity.StartSignupVerificationResult{ChallengeID: mustID(t)}
	rec := do(t, h, http.MethodPost, "/v1/auth/signup/verification/start", allowedOrigin, map[string]any{
		"kind": "email", "identifier": "new@example.com", "locale": "tr",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	decode(t, rec, &body)
	if _, ok := body["challengeId"]; !ok {
		t.Fatalf("body = %#v", body)
	}
	for _, banned := range []string{"sent", "delivered", "email", "queued"} {
		if _, ok := body[banned]; ok {
			t.Fatalf("must not claim delivery via %q: %#v", banned, body)
		}
	}
	lower := strings.ToLower(rec.Body.String())
	if strings.Contains(lower, "sent successfully") || strings.Contains(lower, "delivered") {
		t.Fatalf("claimed delivery: %s", rec.Body.String())
	}
}

func TestIdentityMuxDoesNotRegisterStaffRoutes(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/staff/moderation/reports", "", nil, map[string]string{
		sessionCookieName: "session-raw-token",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("staff on identity mux = %d", rec.Code)
	}
}

func TestStateChangingEventWriteFailureFailsRequest(t *testing.T) {
	h := newTestHandler(t)
	h.SetSecurityRecorder(&failingSecurityRecorder{err: identity.ErrUnavailable})
	h.sessions.issued = identity.IssuedSession{RawToken: "session-raw-token", Session: identity.Session{ID: mustID(t), UserID: mustID(t)}}
	h.sessions.resolved = h.sessions.issued.Session
	rec := do(t, h, http.MethodPost, "/v1/auth/logout", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

type failingSecurityRecorder struct{ err error }

func (f *failingSecurityRecorder) Record(context.Context, identity.SecurityRecord) error {
	if f == nil || f.err == nil {
		return identity.ErrUnavailable
	}
	return f.err
}

func containsEvent(types []identity.AuthSecurityEvent, want identity.AuthSecurityEvent) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}
