package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/needs"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)

	rec := do(t, h, http.MethodPost, "/v1/needs", allowedOrigin, validCreateBody(), map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")

	rec = do(t, h, http.MethodPost, "/v1/needs", "", validCreateBody(), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, "/v1/needs", allowedOrigin, validCreateBody(), authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestCreateAuthenticatedRequesterFromSession(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/needs", allowedOrigin, validCreateBody(), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerDTO
	decode(t, rec, &dto)
	if dto.Status != string(needs.StatusDraft) || dto.Title != "İhtiyaç — حاجة" {
		t.Fatalf("dto = %+v", dto)
	}
	if dto.Description == nil || *dto.Description != "Açıklama" {
		t.Fatalf("description = %v", dto.Description)
	}
	stored, err := h.store.Get(context.Background(), mustParseID(t, dto.NeedID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.RequesterUserID != h.sessions.userID {
		t.Fatalf("stored requester = %s", stored.RequesterUserID)
	}
	assertNoRequesterLeak(t, rec.Body.String(), stored.RequesterUserID.String())
}

func TestCreateRejectsSpoofedRequesterAndStatus(t *testing.T) {
	h := newTestHandler(t)
	body := validCreateBody()
	body["requesterUserId"] = mustID(t).String()
	body["status"] = "open"
	rec := do(t, h, http.MethodPost, "/v1/needs", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	listed, err := h.store.ListByRequester(context.Background(), h.sessions.userID)
	if err != nil || len(listed) != 0 {
		t.Fatal("must not create")
	}
}

func TestOwnerListGetUpdateDeterministic(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/needs", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list status = %d", rec.Code)
	}
	var listed listDTO
	decode(t, rec, &listed)
	if listed.Needs == nil || len(listed.Needs) != 0 {
		t.Fatalf("empty list = %+v", listed)
	}

	first := createOK(t, h)
	h.clock.now = h.clock.now.Add(time.Minute)
	second := createOKBody(t, h, "Second")

	rec = do(t, h, http.MethodGet, "/v1/needs", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	decode(t, rec, &listed)
	if len(listed.Needs) != 2 || listed.Needs[0].NeedID != second.ID.String() || listed.Needs[1].NeedID != first.ID.String() {
		t.Fatalf("list = %+v", listed)
	}
	assertNoRequesterLeak(t, rec.Body.String(), first.RequesterUserID.String())

	rec = do(t, h, http.MethodGet, "/v1/needs?requesterUserId="+mustID(t).String(), "", nil, authedCookies())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("query spoof status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodGet, "/v1/needs/"+first.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPatch, "/v1/needs/"+first.ID.String(), allowedOrigin, map[string]any{
		"title": "Güncellendi",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerDTO
	decode(t, rec, &dto)
	if dto.Title != "Güncellendi" || dto.Status != string(needs.StatusDraft) {
		t.Fatalf("dto = %+v", dto)
	}
}

func TestUnrelatedUserDenied(t *testing.T) {
	h := newTestHandler(t)
	created := createOK(t, h)
	h.sessions.userID = mustID(t)
	rec := do(t, h, http.MethodGet, "/v1/needs/"+created.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
	assertNoSensitiveLeak(t, rec.Body.String())
	if strings.Contains(rec.Body.String(), created.Title) {
		t.Fatal("must not leak title")
	}
	rec = do(t, h, http.MethodPatch, "/v1/needs/"+created.ID.String(), allowedOrigin, map[string]any{
		"title": "Hijack",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch status = %d", rec.Code)
	}
}

func TestLifecycleHTTP(t *testing.T) {
	h := newTestHandler(t)
	created := createOK(t, h)
	rec := do(t, h, http.MethodPost, "/v1/needs/"+created.ID.String()+"/open", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("open status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerDTO
	decode(t, rec, &dto)
	if dto.Status != string(needs.StatusOpen) {
		t.Fatalf("dto = %+v", dto)
	}
	rec = do(t, h, http.MethodPost, "/v1/needs/"+created.ID.String()+"/open", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second open status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/needs/"+created.ID.String()+"/fulfill", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("fulfill status = %d", rec.Code)
	}
	decode(t, rec, &dto)
	if dto.Status != string(needs.StatusFulfilled) {
		t.Fatalf("dto = %+v", dto)
	}
	rec = do(t, h, http.MethodPatch, "/v1/needs/"+created.ID.String(), allowedOrigin, map[string]any{
		"title": "Nope",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("terminal patch status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/needs/"+created.ID.String()+"/cancel", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("terminal cancel status = %d", rec.Code)
	}
}

func TestOpenRejectsClientStatus(t *testing.T) {
	h := newTestHandler(t)
	created := createOK(t, h)
	rec := do(t, h, http.MethodPost, "/v1/needs/"+created.ID.String()+"/open", allowedOrigin, map[string]any{
		"status": "expired",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestBudgetAndLocationHTTPValidation(t *testing.T) {
	h := newTestHandler(t)
	body := validCreateBody()
	body["budget"] = map[string]any{"minAmount": "30", "maxAmount": "10", "currency": "TRY"}
	rec := do(t, h, http.MethodPost, "/v1/needs", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("budget status = %d", rec.Code)
	}
	body = validCreateBody()
	body["location"] = map[string]any{"latitude": 91, "longitude": 29.1}
	rec = do(t, h, http.MethodPost, "/v1/needs", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("lat status = %d", rec.Code)
	}
	body = validCreateBody()
	delete(body, "location")
	rec = do(t, h, http.MethodPost, "/v1/needs", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing location status = %d", rec.Code)
	}
}

func TestPatchRequiresOriginCSRF(t *testing.T) {
	h := newTestHandler(t)
	created := createOK(t, h)
	rec := do(t, h, http.MethodPatch, "/v1/needs/"+created.ID.String(), "", map[string]any{
		"title": "X",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCancelDraft(t *testing.T) {
	h := newTestHandler(t)
	created := createOK(t, h)
	rec := do(t, h, http.MethodPost, "/v1/needs/"+created.ID.String()+"/cancel", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerDTO
	decode(t, rec, &dto)
	if dto.Status != string(needs.StatusCancelled) {
		t.Fatalf("dto = %+v", dto)
	}
}

type testHandler struct {
	*Handler
	sessions *fakeSessions
	store    *needs.MemoryStore
	svc      *needs.Service
	clock    *frozenNow
}

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time { return f.now }

type fakeSessions struct {
	userID needs.ID
	err    error
}

func (f *fakeSessions) Resolve(ctx context.Context, rawToken string) (needs.ID, error) {
	if f.err != nil {
		return needs.ID{}, f.err
	}
	if rawToken != "session-token" {
		return needs.ID{}, ErrUnauthenticated
	}
	return f.userID, nil
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	store := needs.NewMemoryStore()
	clock := &frozenNow{now: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
	svc, err := needs.NewService(store, nil, nil, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{userID: mustID(t)}
	h, err := New(sessions, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: h, sessions: sessions, store: store, svc: svc, clock: clock}
}

func createOK(t *testing.T, h *testHandler) needs.Need {
	t.Helper()
	return createOKBody(t, h, "İhtiyaç — حاجة")
}

func createOKBody(t *testing.T, h *testHandler, title string) needs.Need {
	t.Helper()
	need, err := h.svc.Create(context.Background(), h.sessions.userID, needs.Content{
		Title:       title,
		Description: "Açıklama",
		Location:    needs.Coordinates{Latitude: 36.621, Longitude: 29.116},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.clock.now = h.clock.now.Add(time.Second)
	return need
}

func validCreateBody() map[string]any {
	return map[string]any{
		"title":       "İhtiyaç — حاجة",
		"description": "Açıklama",
		"location": map[string]any{
			"latitude":  36.621,
			"longitude": 29.116,
		},
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

func assertNoRequesterLeak(t *testing.T, body, requesterID string) {
	t.Helper()
	lower := strings.ToLower(body)
	if strings.Contains(lower, "requesteruserid") || strings.Contains(lower, "owneruserid") || strings.Contains(body, requesterID) {
		t.Fatalf("requester identifier leaked in %q", body)
	}
}

func mustID(t *testing.T) needs.ID {
	t.Helper()
	id, err := needs.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustParseID(t *testing.T, raw string) needs.ID {
	t.Helper()
	id, err := needs.ParseID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
