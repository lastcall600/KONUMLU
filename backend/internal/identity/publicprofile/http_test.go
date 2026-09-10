package publicprofile

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/identity"
	"backend/internal/identity/contracts"
)

const allowedOrigin = "https://app.example.test"

func TestHTTPSelfZeroLazyState(t *testing.T) {
	h := newHTTPFixture(t)
	rec := do(t, h, http.MethodGet, "/v1/profile/me", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeDTO(t, rec)
	if body.PublicProfileID == "" || body.PublicProfileID == h.user.ID.String() {
		t.Fatalf("publicProfileId = %q user = %s", body.PublicProfileID, h.user.ID)
	}
	if body.DisplayName != nil {
		t.Fatalf("display = %v", body.DisplayName)
	}
	if body.MemberSince != h.user.CreatedAt.UTC().Format(time.RFC3339) {
		t.Fatalf("memberSince = %q", body.MemberSince)
	}
	assertPrivacy(t, rec.Body.String(), h.user.ID)
}

func TestHTTPUpdateDisplayName(t *testing.T) {
	h := newHTTPFixture(t)
	rec := do(t, h, http.MethodPatch, "/v1/profile/me", allowedOrigin, map[string]any{
		"displayName": "Ada",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeDTO(t, rec)
	if body.DisplayName == nil || *body.DisplayName != "Ada" {
		t.Fatalf("body = %+v", body)
	}
}

func TestHTTPPublicLookupByInternalUserIDIs404(t *testing.T) {
	h := newHTTPFixture(t)
	_ = do(t, h, http.MethodGet, "/v1/profile/me", "", nil, authedCookies())
	rec := do(t, h, http.MethodGet, "/v1/public/profiles/"+h.user.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPPublicLookup(t *testing.T) {
	h := newHTTPFixture(t)
	me := do(t, h, http.MethodGet, "/v1/profile/me", "", nil, authedCookies())
	id := decodeDTO(t, me).PublicProfileID
	rec := do(t, h, http.MethodGet, "/v1/public/profiles/"+id, "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeDTO(t, rec)
	if body.PublicProfileID != id {
		t.Fatalf("body = %+v", body)
	}
	assertPrivacy(t, rec.Body.String(), h.user.ID)
}

func TestHTTPMalformedPublicID(t *testing.T) {
	h := newHTTPFixture(t)
	rec := do(t, h, http.MethodGet, "/v1/public/profiles/not-a-uuid", "", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHTTPMissingProfile(t *testing.T) {
	h := newHTTPFixture(t)
	rec := do(t, h, http.MethodGet, "/v1/public/profiles/"+mustID(t).String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPDisabledNotExposed(t *testing.T) {
	h := newHTTPFixture(t)
	me := do(t, h, http.MethodGet, "/v1/profile/me", "", nil, authedCookies())
	id := decodeDTO(t, me).PublicProfileID
	at := time.Now().UTC()
	user := h.user
	user.DisabledAt = &at
	h.store.PutUser(user)
	rec := do(t, h, http.MethodGet, "/v1/public/profiles/"+id, "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHTTPRestrictedAndRemovedArePrivacy404(t *testing.T) {
	h := newHTTPFixture(t)
	me := do(t, h, http.MethodGet, "/v1/profile/me", "", nil, authedCookies())
	id := decodeDTO(t, me).PublicProfileID
	publicID, err := ParsePublicID(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ApplyModerationState(context.Background(), contracts.ApplyPublicProfileModerationInput{
		PublicProfileID: toContractID(publicID), State: contracts.ModerationStateRestricted,
	}); err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodGet, "/v1/public/profiles/"+id, "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("restricted status = %d body=%s", rec.Code, rec.Body.String())
	}
	self := do(t, h, http.MethodGet, "/v1/profile/me", "", nil, authedCookies())
	if self.Code != http.StatusOK {
		t.Fatalf("self status = %d", self.Code)
	}
	assertPrivacy(t, rec.Body.String(), h.user.ID)
	if err := h.svc.ApplyModerationState(context.Background(), contracts.ApplyPublicProfileModerationInput{
		PublicProfileID: toContractID(publicID), State: contracts.ModerationStateRemoved,
	}); err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodGet, "/v1/public/profiles/"+id, "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("removed status = %d", rec.Code)
	}
	if err := h.svc.ClearModerationState(context.Background(), toContractID(publicID)); err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodGet, "/v1/public/profiles/"+id, "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("restored status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPRejectsClientModerationState(t *testing.T) {
	h := newHTTPFixture(t)
	state := "restricted"
	rec := do(t, h, http.MethodPatch, "/v1/profile/me", allowedOrigin, map[string]any{
		"displayName":     "Ada",
		"moderationState": state,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPCrossUserPatchRejected(t *testing.T) {
	h := newHTTPFixture(t)
	other := mustID(t)
	rec := do(t, h, http.MethodPatch, "/v1/profile/me?userId="+other.String(), allowedOrigin, map[string]any{
		"displayName": "Nope",
		"userId":      other.String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPPatchRequiresCSRF(t *testing.T) {
	h := newHTTPFixture(t)
	rec := do(t, h, http.MethodPatch, "/v1/profile/me", allowedOrigin, map[string]any{
		"displayName": "Ada",
	}, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

type httpFixture struct {
	handler *Handler
	store   *MemoryStore
	svc     *Service
	user    identity.User
}

func newHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	store := NewMemoryStore()
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	user := identity.User{ID: mustID(t), CreatedAt: now, UpdatedAt: now}
	store.PutUser(user)
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(stubSessions{userID: user.ID}, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &httpFixture{handler: h, store: store, svc: svc, user: user}
}

func (h *httpFixture) mux() *http.ServeMux {
	mux := http.NewServeMux()
	h.handler.Register(mux)
	return mux
}

type stubSessions struct {
	userID identity.ID
	err    error
}

func (s stubSessions) Resolve(_ context.Context, rawToken string) (identity.ID, error) {
	if s.err != nil {
		return identity.ID{}, s.err
	}
	if rawToken == "" {
		return identity.ID{}, ErrUnauthenticated
	}
	return s.userID, nil
}

func do(t *testing.T, h *httpFixture, method, path, origin string, body map[string]any, cookies map[string]string, opts ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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

func authedCookies() map[string]string {
	return map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}
}

func withCSRF(token string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set(csrfHeaderName, token)
	}
}

func decodeDTO(t *testing.T, rec *httptest.ResponseRecorder) profileDTO {
	t.Helper()
	var body profileDTO
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return body
}

func assertPrivacy(t *testing.T, raw string, userID identity.ID) {
	t.Helper()
	lower := strings.ToLower(raw)
	if strings.Contains(lower, `"userid"`) || strings.Contains(raw, userID.String()) {
		t.Fatalf("user id leaked: %s", raw)
	}
	if strings.Contains(lower, "email") || strings.Contains(lower, "phone") {
		t.Fatalf("identifier leaked: %s", raw)
	}
}
