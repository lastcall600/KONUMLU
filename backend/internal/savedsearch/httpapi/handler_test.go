package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/savedsearch"
)

const allowedOrigin = "https://app.example.test"

func TestMutationsRequireAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)
	body := map[string]any{"name": "Bisiklet"}
	rec := do(t, h, http.MethodPost, "/v1/saved-searches", allowedOrigin, body, map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/saved-searches", "", body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/saved-searches", allowedOrigin, body, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestReadsRequireSession(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/saved-searches", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("list status = %d", rec.Code)
	}
	id := mustID(t)
	rec = do(t, h, http.MethodGet, "/v1/saved-searches/"+id.String(), "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("get status = %d", rec.Code)
	}
}

func TestCreateListGetDelete(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/saved-searches", allowedOrigin, map[string]any{
		"name": "Bisiklet", "q": "bike", "minPrice": "10", "maxPrice": "90", "currency": "TRY",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created savedSearchDTO
	decode(t, rec, &created)
	if created.ID == "" || created.Name != "Bisiklet" || created.Q == nil || *created.Q != "bike" {
		t.Fatalf("created = %+v", created)
	}

	rec = do(t, h, http.MethodGet, "/v1/saved-searches", "", nil, authedCookies())
	var list listDTO
	decode(t, rec, &list)
	if len(list.SavedSearches) != 1 || list.SavedSearches[0].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}

	rec = do(t, h, http.MethodGet, "/v1/saved-searches/"+created.ID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodDelete, "/v1/saved-searches/"+created.ID, allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/saved-searches/"+created.ID, "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
}

func TestOwnershipIsolationPrivacy404(t *testing.T) {
	h := newTestHandler(t)
	other, err := savedsearch.NewID()
	if err != nil {
		t.Fatal(err)
	}
	row, err := h.svc.Create(context.Background(), other, savedsearch.CreateInput{Name: "Gizli"})
	if err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodGet, "/v1/saved-searches/"+row.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign get status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
	rec = do(t, h, http.MethodDelete, "/v1/saved-searches/"+row.ID.String(), allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign delete status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
	rec = do(t, h, http.MethodGet, "/v1/saved-searches", "", nil, authedCookies())
	var list listDTO
	decode(t, rec, &list)
	if len(list.SavedSearches) != 0 {
		t.Fatalf("list leaked = %+v", list)
	}
}

func TestInvalidFilterRejected(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/saved-searches", allowedOrigin, map[string]any{
		"name": "Bad", "minPrice": "abc",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
}

func TestCompleteViewportAccepted(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/saved-searches", allowedOrigin, map[string]any{
		"name": "Alan", "north": 37.1, "south": 36.4, "east": 29.5, "west": 29.0,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created savedSearchDTO
	decode(t, rec, &created)
	if created.North == nil || created.South == nil || created.East == nil || created.West == nil {
		t.Fatalf("viewport = %+v", created)
	}
}

func TestPartialViewportRejected(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/saved-searches", allowedOrigin, map[string]any{
		"name": "Alan", "north": 37.1,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
}

func TestCursorCannotBeStored(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/saved-searches", allowedOrigin, map[string]any{
		"name": "Cursor", "q": "bike", "cursor": "opaque-cursor",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
	rec = do(t, h, http.MethodGet, "/v1/saved-searches", "", nil, authedCookies())
	var list listDTO
	decode(t, rec, &list)
	if len(list.SavedSearches) != 0 {
		t.Fatalf("cursor body stored = %+v", list)
	}
}

func TestUserIDFromSessionOnly(t *testing.T) {
	h := newTestHandler(t)
	foreign, err := savedsearch.NewID()
	if err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodPost, "/v1/saved-searches", allowedOrigin, map[string]any{
		"name": "Hijack", "userId": foreign.String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
}

func TestOwnedMissingDeleteIsPrivacySafe404(t *testing.T) {
	h := newTestHandler(t)
	id := mustID(t)
	rec := do(t, h, http.MethodDelete, "/v1/saved-searches/"+id.String(), allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
}

func TestSessionInfraIs503(t *testing.T) {
	h := newTestHandler(t)
	h.sessions.err = errors.New("valkey timeout")
	h.Handler.sessions = h.sessions
	rec := do(t, h, http.MethodPost, "/v1/saved-searches", allowedOrigin, map[string]any{"name": "X"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
}

type stubSessions struct {
	userID savedsearch.ID
	err    error
}

func (s stubSessions) Resolve(_ context.Context, rawToken string) (savedsearch.ID, error) {
	if s.err != nil {
		return savedsearch.ID{}, s.err
	}
	if rawToken == "" {
		return savedsearch.ID{}, ErrUnauthenticated
	}
	return s.userID, nil
}

type testHandler struct {
	*Handler
	sessions stubSessions
	store    *savedsearch.MemoryStore
	svc      *savedsearch.Service
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	user, err := savedsearch.NewID()
	if err != nil {
		t.Fatal(err)
	}
	store := savedsearch.NewMemoryStore()
	svc, err := savedsearch.NewService(store, func() time.Time { return time.Unix(20, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	sessions := stubSessions{userID: user}
	h, err := New(sessions, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: h, sessions: sessions, store: store, svc: svc}
}

func mustID(t *testing.T) savedsearch.ID {
	t.Helper()
	id, err := savedsearch.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
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

func do(t *testing.T, h *testHandler, method, path, origin string, body any, cookies map[string]string, opts ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for name, value := range cookies {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	for _, opt := range opts {
		opt(r)
	}
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(rec, r)
	return rec
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
		t.Fatalf("error = %q want %q", body.Error, code)
	}
}
