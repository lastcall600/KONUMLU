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

	"backend/internal/businesses"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)

	rec := do(t, h, http.MethodPost, "/v1/businesses", allowedOrigin, validCreateBody(), map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")

	rec = do(t, h, http.MethodPost, "/v1/businesses", "", validCreateBody(), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")

	rec = do(t, h, http.MethodPost, "/v1/businesses", allowedOrigin, validCreateBody(), authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")
}

func TestCreateAuthenticatedOwnerFromSession(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/businesses", allowedOrigin, validCreateBody(), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerDTO
	decode(t, rec, &dto)
	if dto.Status != string(businesses.StatusDraft) || dto.DisplayName != "Кафе البحر" {
		t.Fatalf("dto = %+v", dto)
	}
	if dto.Description == nil || *dto.Description != "Açıklama" {
		t.Fatalf("description = %v", dto.Description)
	}
	stored, err := h.store.Get(context.Background(), mustParseID(t, dto.BusinessID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.OwnerUserID != h.sessions.userID {
		t.Fatalf("stored owner = %s", stored.OwnerUserID)
	}
	assertNoOwnerLeak(t, rec.Body.String(), stored.OwnerUserID.String())
}

func TestCreateRejectsSpoofedOwnerAndStatus(t *testing.T) {
	h := newTestHandler(t)
	body := validCreateBody()
	body["ownerUserId"] = mustID(t).String()
	body["status"] = "active"
	rec := do(t, h, http.MethodPost, "/v1/businesses", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := h.store.GetByOwner(context.Background(), h.sessions.userID); !errors.Is(err, businesses.ErrNotFound) {
		t.Fatal("must not create")
	}
}

func TestCreateOnePerUser(t *testing.T) {
	h := newTestHandler(t)
	createOK(t, h)
	rec := do(t, h, http.MethodPost, "/v1/businesses", allowedOrigin, validCreateBody(), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetMineDeterministic(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/businesses/mine", "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty mine status = %d", rec.Code)
	}
	created := createOK(t, h)
	rec = do(t, h, http.MethodGet, "/v1/businesses/mine", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerDTO
	decode(t, rec, &dto)
	if dto.BusinessID != created.ID.String() || dto.Status != string(businesses.StatusDraft) {
		t.Fatalf("dto = %+v", dto)
	}
	rec = do(t, h, http.MethodGet, "/v1/businesses/mine?ownerUserId="+mustID(t).String(), "", nil, authedCookies())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("query spoof status = %d", rec.Code)
	}
}

func TestOwnerGetAndPatch(t *testing.T) {
	h := newTestHandler(t)
	created := createOK(t, h)
	rec := do(t, h, http.MethodGet, "/v1/businesses/"+created.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPatch, "/v1/businesses/"+created.ID.String(), allowedOrigin, map[string]any{
		"displayName": "Kafe Yeni",
		"description": "Yeni açıklama",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerDTO
	decode(t, rec, &dto)
	if dto.DisplayName != "Kafe Yeni" || dto.Status != string(businesses.StatusDraft) {
		t.Fatalf("dto = %+v", dto)
	}
}

func TestUnrelatedUserDenied(t *testing.T) {
	h := newTestHandler(t)
	created := createOK(t, h)
	h.sessions.userID = mustID(t)
	rec := do(t, h, http.MethodGet, "/v1/businesses/"+created.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
	assertNoSensitiveLeak(t, rec.Body.String())
	if strings.Contains(rec.Body.String(), created.DisplayName) {
		t.Fatal("must not leak display name")
	}
	rec = do(t, h, http.MethodPatch, "/v1/businesses/"+created.ID.String(), allowedOrigin, map[string]any{
		"displayName": "Hijack",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch status = %d", rec.Code)
	}
}

func TestActivateAndCloseLifecycle(t *testing.T) {
	h := newTestHandler(t)
	created := createOK(t, h)
	rec := do(t, h, http.MethodPost, "/v1/businesses/"+created.ID.String()+"/activate", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("activate status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerDTO
	decode(t, rec, &dto)
	if dto.Status != string(businesses.StatusActive) {
		t.Fatalf("dto = %+v", dto)
	}
	rec = do(t, h, http.MethodPost, "/v1/businesses/"+created.ID.String()+"/activate", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second activate status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/businesses/"+created.ID.String()+"/close", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("close status = %d body=%s", rec.Code, rec.Body.String())
	}
	decode(t, rec, &dto)
	if dto.Status != string(businesses.StatusClosed) {
		t.Fatalf("dto = %+v", dto)
	}
	rec = do(t, h, http.MethodPost, "/v1/businesses/"+created.ID.String()+"/activate", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("reopen status = %d", rec.Code)
	}
}

func TestActivateRejectsClientStatus(t *testing.T) {
	h := newTestHandler(t)
	created := createOK(t, h)
	rec := do(t, h, http.MethodPost, "/v1/businesses/"+created.ID.String()+"/activate", allowedOrigin, map[string]any{
		"status": "suspended",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPublicActiveOnlyAndPrivacy(t *testing.T) {
	h := newTestHandler(t)
	draft := createOK(t, h)
	rec := do(t, h, http.MethodGet, "/v1/public/businesses/"+draft.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("draft public status = %d", rec.Code)
	}

	active := createOKFor(t, h, mustID(t))
	if _, err := h.svc.Activate(context.Background(), active.OwnerUserID, active.ID); err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodGet, "/v1/public/businesses/"+active.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("active public status = %d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	decode(t, rec, &raw)
	for _, banned := range []string{"ownerUserId", "owner_user_id", "userId", "status", "moderationState", "verificationStatus", "phone", "email"} {
		if _, ok := raw[banned]; ok {
			t.Fatalf("public DTO leaked %s: %v", banned, raw)
		}
	}
	if raw["businessId"] != active.ID.String() || raw["displayName"] != active.DisplayName {
		t.Fatalf("public = %v", raw)
	}
	assertNoOwnerLeak(t, rec.Body.String(), active.OwnerUserID.String())

	closed := createOKFor(t, h, mustID(t))
	if _, err := h.svc.Close(context.Background(), closed.OwnerUserID, closed.ID); err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodGet, "/v1/public/businesses/"+closed.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("closed public status = %d", rec.Code)
	}

	suspended := createOKFor(t, h, mustID(t))
	suspended.Status = businesses.StatusSuspended
	if err := h.store.Update(context.Background(), suspended, suspended.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodGet, "/v1/public/businesses/"+suspended.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("suspended public status = %d", rec.Code)
	}
}

func TestPatchRequiresOriginCSRF(t *testing.T) {
	h := newTestHandler(t)
	created := createOK(t, h)
	rec := do(t, h, http.MethodPatch, "/v1/businesses/"+created.ID.String(), "", map[string]any{
		"displayName": "X",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

type testHandler struct {
	*Handler
	sessions *fakeSessions
	store    *businesses.MemoryStore
	svc      *businesses.Service
}

type fakeSessions struct {
	userID businesses.ID
	err    error
}

func (f *fakeSessions) Resolve(ctx context.Context, rawToken string) (businesses.ID, error) {
	if f.err != nil {
		return businesses.ID{}, f.err
	}
	if rawToken != "session-token" {
		return businesses.ID{}, ErrUnauthenticated
	}
	return f.userID, nil
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	store := businesses.NewMemoryStore()
	clock := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	svc, err := businesses.NewService(store, nil, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{userID: mustID(t)}
	h, err := New(sessions, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: h, sessions: sessions, store: store, svc: svc}
}

func createOK(t *testing.T, h *testHandler) businesses.Profile {
	t.Helper()
	return createOKFor(t, h, h.sessions.userID)
}

func createOKFor(t *testing.T, h *testHandler, owner businesses.ID) businesses.Profile {
	t.Helper()
	profile, err := h.svc.Create(context.Background(), owner, businesses.ProfileContent{
		DisplayName: "Кафе البحر",
		Description: "Açıklama",
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func validCreateBody() map[string]any {
	return map[string]any{
		"displayName": "Кафе البحر",
		"description": "Açıklama",
	}
}

func authedCookies() map[string]string {
	return map[string]string{
		sessionCookieName: "session-token",
		csrfCookieName:    "csrf-token",
	}
}

type headerOption func(*http.Request)

func withCSRF(token string) headerOption {
	return func(r *http.Request) {
		r.Header.Set(csrfHeaderName, token)
	}
}

func do(t *testing.T, h interface{ Register(*http.ServeMux) }, method, path, origin string, body any, cookies map[string]string, opts ...headerOption) *httptest.ResponseRecorder {
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

func assertNoSensitiveLeak(t *testing.T, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	banned := []string{"postgres", "sql:", "minio", "s3", "stack", "panic", "secret"}
	for _, w := range banned {
		if strings.Contains(lower, w) {
			t.Fatalf("sensitive token %q leaked in %q", w, body)
		}
	}
}

func assertNoOwnerLeak(t *testing.T, body, ownerID string) {
	t.Helper()
	if strings.Contains(strings.ToLower(body), "owneruserid") || strings.Contains(body, ownerID) {
		t.Fatalf("owner identifier leaked in %q", body)
	}
}

func mustID(t *testing.T) businesses.ID {
	t.Helper()
	id, err := businesses.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustParseID(t *testing.T, raw string) businesses.ID {
	t.Helper()
	id, err := businesses.ParseID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
