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

	"backend/internal/favorites"
	listingcontracts "backend/internal/listings/contracts"
)

const allowedOrigin = "https://app.example.test"

func TestMutationsRequireAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)

	rec := do(t, h, http.MethodPost, "/v1/favorites/listings/"+listing.String(), allowedOrigin, nil, map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, "/v1/favorites/listings/"+listing.String(), "", nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, "/v1/favorites/listings/"+listing.String(), allowedOrigin, nil, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodDelete, "/v1/favorites/listings/"+listing.String(), allowedOrigin, nil, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete missing CSRF status = %d", rec.Code)
	}
}

func TestReadsRequireSession(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)
	rec := do(t, h, http.MethodGet, "/v1/favorites/listings/"+listing.String(), "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("get status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/v1/favorites/listings", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("list status = %d", rec.Code)
	}
}

func TestAddGetDeleteIdempotent(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)
	path := "/v1/favorites/listings/" + listing.String()

	rec := do(t, h, http.MethodPost, path, allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("add status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, path, allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("add again status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodGet, path, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	var state favoriteStateDTO
	decode(t, rec, &state)
	if !state.Favorited {
		t.Fatalf("state = %+v", state)
	}

	rec = do(t, h, http.MethodGet, "/v1/favorites/listings", "", nil, authedCookies())
	var list favoriteListDTO
	decode(t, rec, &list)
	if len(list.Listings) != 1 || list.Listings[0].ListingID != listing.String() {
		t.Fatalf("list = %+v", list)
	}

	rec = do(t, h, http.MethodDelete, path, allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodDelete, path, allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete again status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, path, "", nil, authedCookies())
	decode(t, rec, &state)
	if state.Favorited {
		t.Fatalf("after delete = %+v", state)
	}
}

func TestAddNonPublicIsPrivacySafe404(t *testing.T) {
	h := newTestHandler(t)
	listing := mustID(t)
	rec := do(t, h, http.MethodPost, "/v1/favorites/listings/"+listing.String(), allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
}

func TestListOmitsNonPublicWithoutExposingThem(t *testing.T) {
	h := newTestHandler(t)
	pub := mustPublished(t, h)
	hidden := mustID(t)
	if err := h.store.Add(context.Background(), favorites.Favorite{
		UserID: h.sessions.userID, ListingID: hidden, CreatedAt: time.Unix(1, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Add(context.Background(), h.sessions.userID, pub); err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodGet, "/v1/favorites/listings", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list favoriteListDTO
	decode(t, rec, &list)
	if len(list.Listings) != 1 || list.Listings[0].ListingID != pub.String() {
		t.Fatalf("list = %+v", list)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(hidden.String())) {
		t.Fatal("hidden listing id leaked")
	}
}

func TestOtherUserFavoritesAreNotVisible(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)
	other, err := favorites.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Add(context.Background(), other, listing); err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodGet, "/v1/favorites/listings", "", nil, authedCookies())
	var list favoriteListDTO
	decode(t, rec, &list)
	if len(list.Listings) != 0 {
		t.Fatalf("list = %+v", list)
	}
}

type stubSessions struct {
	userID favorites.ID
	err    error
}

func (s stubSessions) Resolve(_ context.Context, rawToken string) (favorites.ID, error) {
	if s.err != nil {
		return favorites.ID{}, s.err
	}
	if rawToken == "" {
		return favorites.ID{}, ErrUnauthenticated
	}
	return s.userID, nil
}

type stubListings struct {
	snaps map[listingcontracts.ID]listingcontracts.ListingSnapshot
}

func (s *stubListings) GetListingSnapshot(_ context.Context, listingID listingcontracts.ID) (listingcontracts.ListingSnapshot, error) {
	snap, ok := s.snaps[listingID]
	if !ok {
		return listingcontracts.ListingSnapshot{}, listingcontracts.ErrNotFound
	}
	return snap, nil
}

type testHandler struct {
	*Handler
	sessions stubSessions
	store    *favorites.MemoryStore
	listings *stubListings
	svc      *favorites.Service
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	user, err := favorites.NewID()
	if err != nil {
		t.Fatal(err)
	}
	store := favorites.NewMemoryStore()
	listings := &stubListings{snaps: map[listingcontracts.ID]listingcontracts.ListingSnapshot{}}
	svc, err := favorites.NewService(store, listings, func() time.Time { return time.Unix(20, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	sessions := stubSessions{userID: user}
	h, err := New(sessions, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: h, sessions: sessions, store: store, listings: listings, svc: svc}
}

func mustPublished(t *testing.T, h *testHandler) favorites.ID {
	t.Helper()
	id := mustID(t)
	h.listings.snaps[listingcontracts.ID(id)] = listingcontracts.ListingSnapshot{
		ID:     listingcontracts.ID(id),
		Status: listingcontracts.StatusPublished,
	}
	return id
}

func mustID(t *testing.T) favorites.ID {
	t.Helper()
	id, err := favorites.NewID()
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

func TestSessionInfraIs503(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)
	h.sessions.err = errors.New("valkey timeout")
	h.Handler.sessions = h.sessions
	rec := do(t, h, http.MethodPost, "/v1/favorites/listings/"+listing.String(), allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
}
