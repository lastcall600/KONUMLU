package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"

	"backend/internal/identity"
)

func TestBeginPasskeyRegisterRequiresAuthenticatedSession(t *testing.T) {
	h := newTestHandler(t)
	h.registration.begin = validRegisterBegin()

	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/register/begin", allowedOrigin, nil, map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unauthenticated")
	if h.registration.begins != 0 {
		t.Fatal("missing session must not begin registration")
	}

	h.sessions.resolveErr = identity.ErrUnauthenticated
	rec = doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid session status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")
	assertNoSensitiveLeak(t, rec.Body.String())
	if h.registration.begins != 0 {
		t.Fatal("invalid session must not begin registration")
	}
}

func TestBeginPasskeyRegisterRequiresCSRF(t *testing.T) {
	h := newAuthedRegisterHandler(t)
	h.registration.begin = validRegisterBegin()

	rec := do(t, h, http.MethodPost, "/v1/auth/passkey/register/begin", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")

	rec = do(t, h, http.MethodPost, "/v1/auth/passkey/register/begin", allowedOrigin, nil, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("other-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatch CSRF status = %d", rec.Code)
	}
	if h.registration.begins != 0 {
		t.Fatal("CSRF failure must not begin registration")
	}
}

func TestBeginPasskeyRegisterUsesSessionUserNotClientUserID(t *testing.T) {
	h := newAuthedRegisterHandler(t)
	h.registration.begin = validRegisterBegin()
	clientUser := mustID(t)

	rec := doRegisterBegin(t, h, map[string]any{
		"userId":      clientUser.String(),
		"user_id":     clientUser.String(),
		"name":        "label-name",
		"displayName": "label-display",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if h.registration.lastBegin.UserID != h.sessions.resolved.UserID {
		t.Fatalf("begin user = %s, want session %s", h.registration.lastBegin.UserID, h.sessions.resolved.UserID)
	}
	if h.registration.lastBegin.UserID == clientUser {
		t.Fatal("must not use client-supplied user id")
	}
	if h.registration.lastBegin.Name != "label-name" || h.registration.lastBegin.DisplayName != "label-display" {
		t.Fatalf("labels = %+v", h.registration.lastBegin)
	}

	var body registerBeginResponse
	decode(t, rec, &body)
	if body.CeremonyToken != "reg-ceremony-token" {
		t.Fatalf("ceremonyToken = %q", body.CeremonyToken)
	}

	rec = doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("omitted labels status = %d", rec.Code)
	}
	want := h.sessions.resolved.UserID.String()
	if h.registration.lastBegin.Name != want || h.registration.lastBegin.DisplayName != want {
		t.Fatalf("default labels = name %q display %q, want %q", h.registration.lastBegin.Name, h.registration.lastBegin.DisplayName, want)
	}
}

func TestBeginPasskeyRegisterReturnsExcludedActiveCredentials(t *testing.T) {
	h := newAuthedRegisterHandler(t)
	h.registration.begin = identity.BeginRegistrationResult{
		RawToken: "reg-ceremony-token",
		Creation: &protocol.CredentialCreation{
			Response: protocol.PublicKeyCredentialCreationOptions{
				AuthenticatorSelection: protocol.AuthenticatorSelection{
					UserVerification: protocol.VerificationRequired,
				},
				CredentialExcludeList: []protocol.CredentialDescriptor{{
					Type:         protocol.PublicKeyCredentialType,
					CredentialID: protocol.URLEncodedBase64{0x0a, 0x0b},
				}},
			},
		},
	}
	rec := doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body registerBeginResponse
	decode(t, rec, &body)
	if body.PublicKey.AuthenticatorSelection.UserVerification != protocol.VerificationRequired {
		t.Fatalf("uv = %q", body.PublicKey.AuthenticatorSelection.UserVerification)
	}
	if len(body.PublicKey.CredentialExcludeList) != 1 {
		t.Fatalf("exclude = %+v", body.PublicKey.CredentialExcludeList)
	}
}

func TestFinishPasskeyRegisterSuccess(t *testing.T) {
	h := newAuthedRegisterHandler(t)
	h.registration.finish = identity.PasskeyCredential{ID: mustID(t), UserID: h.sessions.resolved.UserID}

	rec := doRegisterFinish(t, h, "c-token", map[string]any{"type": "public-key"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if h.registration.finishes != 1 {
		t.Fatalf("finishes = %d", h.registration.finishes)
	}
	if h.registration.lastFinish.UserID != h.sessions.resolved.UserID {
		t.Fatal("finish must bind the authenticated session user")
	}
	if h.registration.lastFinish.RawToken != "c-token" {
		t.Fatalf("token = %q", h.registration.lastFinish.RawToken)
	}
	var body registerFinishResponse
	decode(t, rec, &body)
	if !body.OK {
		t.Fatalf("response = %+v", body)
	}
	raw := strings.ToLower(rec.Body.String())
	for _, w := range []string{"publickey", "credentialid", "signcount", "attestation"} {
		if strings.Contains(raw, w) {
			t.Fatalf("success leaked %q: %s", w, rec.Body.String())
		}
	}
}

func TestFinishPasskeyRegisterRequiresSameAuthenticatedUser(t *testing.T) {
	h := newAuthedRegisterHandler(t)
	owner := h.sessions.resolved.UserID
	h.registration.begin = validRegisterBegin()
	if rec := doRegisterBegin(t, h, nil); rec.Code != http.StatusOK {
		t.Fatalf("begin status = %d", rec.Code)
	}
	if h.registration.lastBegin.UserID != owner {
		t.Fatal("begin must use owner session")
	}

	other := mustID(t)
	h.sessions.resolved = identity.Session{UserID: other}
	h.registration.finishErr = identity.ErrUnauthenticated
	rec := doRegisterFinish(t, h, "reg-ceremony-token", map[string]any{"type": "public-key"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cross-user status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
	assertNoSensitiveLeak(t, rec.Body.String())
	if h.registration.lastFinish.UserID != other {
		t.Fatal("finish must pass the current session user, not the ceremony owner")
	}
	if h.registration.lastFinish.UserID == owner {
		t.Fatal("cross-user finish must not run as the original owner")
	}
}

func TestFinishPasskeyRegisterReplayFails(t *testing.T) {
	h := newAuthedRegisterHandler(t)
	rec := doRegisterFinish(t, h, "c-token", map[string]any{"type": "public-key"})
	if rec.Code != http.StatusOK {
		t.Fatalf("first finish status = %d", rec.Code)
	}
	h.registration.finishErr = identity.ErrUnauthenticated
	rec = doRegisterFinish(t, h, "c-token", map[string]any{"type": "public-key"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("replay status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "bad_request")
	assertNoSensitiveLeak(t, rec.Body.String())
}

func TestFinishPasskeyRegisterDuplicateCredentialConflict(t *testing.T) {
	h := newAuthedRegisterHandler(t)
	h.registration.finishErr = identity.ErrCredentialConflict
	rec := doRegisterFinish(t, h, "c-token", map[string]any{"type": "public-key"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "conflict")
	assertNoSensitiveLeak(t, rec.Body.String())
	if strings.Contains(strings.ToLower(rec.Body.String()), "credential") {
		t.Fatal("must not leak credential conflict details")
	}
}

func TestPasskeyRegisterMalformedJSONIsBadRequest(t *testing.T) {
	h := newAuthedRegisterHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/passkey/register/begin", strings.NewReader("{"))
	req.Header.Set("Origin", allowedOrigin)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-raw-token"})
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-token"})
	req.Header.Set(csrfHeaderName, "csrf-token")
	rec := httptest.NewRecorder()
	h.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("begin status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/auth/passkey/register/finish", strings.NewReader("{"))
	req.Header.Set("Origin", allowedOrigin)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-raw-token"})
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-token"})
	req.Header.Set(csrfHeaderName, "csrf-token")
	rec = httptest.NewRecorder()
	h.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("finish status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "bad_request")
	if h.registration.finishes != 0 {
		t.Fatal("malformed finish must not consume a ceremony")
	}
}

func TestPasskeyRegisterInfrastructureMapsTo503(t *testing.T) {
	h := newAuthedRegisterHandler(t)
	h.registration.beginErr = identity.ErrUnavailable
	rec := doRegisterBegin(t, h, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("begin status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())

	h.registration.beginErr = nil
	h.registration.finishErr = identity.ErrUnavailable
	rec = doRegisterFinish(t, h, "c-token", map[string]any{"type": "public-key"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("finish status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())
}

func TestPasskeyRegisterErrorDoesNotLeakInternals(t *testing.T) {
	h := newAuthedRegisterHandler(t)
	h.registration.finishErr = identity.ErrUnauthenticated
	rec := doRegisterFinish(t, h, "secret-ceremony-token", map[string]any{
		"type":              "public-key",
		"attestationObject": "attestation-blob",
		"clientDataJSON":    "challenge-material",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	assertNoSensitiveLeak(t, body)
	if strings.Contains(body, "secret-ceremony-token") ||
		strings.Contains(body, "attestation") ||
		strings.Contains(body, "session-raw-token") {
		t.Fatalf("leaked sensitive material: %s", body)
	}
}

func newAuthedRegisterHandler(t *testing.T) *testHandler {
	t.Helper()
	h := newTestHandler(t)
	h.sessions.resolved = identity.Session{UserID: mustID(t)}
	h.registration.begin = validRegisterBegin()
	return h
}

func validRegisterBegin() identity.BeginRegistrationResult {
	return identity.BeginRegistrationResult{
		RawToken: "reg-ceremony-token",
		Creation: &protocol.CredentialCreation{
			Response: protocol.PublicKeyCredentialCreationOptions{
				AuthenticatorSelection: protocol.AuthenticatorSelection{
					UserVerification: protocol.VerificationRequired,
				},
			},
		},
	}
}

func doRegisterBegin(t *testing.T, h *testHandler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/v1/auth/passkey/register/begin", allowedOrigin, body, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
}

func doRegisterFinish(t *testing.T, h *testHandler, token string, credential map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, h, http.MethodPost, "/v1/auth/passkey/register/finish", allowedOrigin, map[string]any{
		"ceremonyToken": token,
		"credential":    credential,
	}, map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}, withCSRF("csrf-token"))
}
