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

	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/messaging"
)

const allowedOrigin = "https://app.example.test"

func TestMutationsRequireAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)

	rec := do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{"listingId": listing.String()}, nil, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations", "", map[string]string{"listingId": listing.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{"listingId": listing.String()}, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestReadsRequireSession(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/messaging/conversations", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("list status = %d", rec.Code)
	}
}

func TestCreateConversationPublishedAndIdempotent(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)
	rec := do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{"listingId": listing.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var first conversationDTO
	decode(t, rec, &first)
	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{"listingId": listing.String()}, authedCookies(), withCSRF("csrf-token"))
	var second conversationDTO
	decode(t, rec, &second)
	if first.ConversationID != second.ConversationID {
		t.Fatalf("idempotent mismatch %s %s", first.ConversationID, second.ConversationID)
	}
}

func TestCreateSelfAndNonPublic(t *testing.T) {
	h := newTestHandler(t)
	own := mustID(t)
	h.listings.setPublished(own, h.sessions.userID)
	rec := do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{"listingId": own.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("self status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")

	missing := mustID(t)
	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{"listingId": missing.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
}

func TestParticipantSendAndForeign404(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)
	rec := do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{"listingId": listing.String()}, authedCookies(), withCSRF("csrf-token"))
	var conv conversationDTO
	decode(t, rec, &conv)

	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations/"+conv.ConversationID+"/messages", allowedOrigin, map[string]string{"body": "merhaba"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("send status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/messaging/conversations/"+conv.ConversationID+"/messages", "", nil, authedCookies())
	var list messageListDTO
	decode(t, rec, &list)
	if len(list.Messages) != 1 || list.Messages[0].Body != "merhaba" {
		t.Fatalf("messages = %+v", list)
	}

	h.sessions.userID = mustID(t)
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodGet, "/v1/messaging/conversations/"+conv.ConversationID, "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign get status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations/"+conv.ConversationID+"/messages", allowedOrigin, map[string]string{"body": "nope"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign send status = %d", rec.Code)
	}
}

func TestEmptyAndOversizeMessageRejected(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)
	rec := do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{"listingId": listing.String()}, authedCookies(), withCSRF("csrf-token"))
	var conv conversationDTO
	decode(t, rec, &conv)
	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations/"+conv.ConversationID+"/messages", allowedOrigin, map[string]string{"body": "  "}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations/"+conv.ConversationID+"/messages", allowedOrigin, map[string]string{"body": strings.Repeat("a", messaging.MaxMessageBytes+1)}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversize status = %d", rec.Code)
	}
}

func TestUnreadReadAndNewestOrdering(t *testing.T) {
	h := newTestHandler(t)
	seller := mustID(t)
	listingA := mustID(t)
	listingB := mustID(t)
	h.listings.setPublished(listingA, seller)
	h.listings.setPublished(listingB, seller)
	rec := do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{"listingId": listingA.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create A status = %d body=%s", rec.Code, rec.Body.String())
	}
	var first conversationDTO
	decode(t, rec, &first)
	h.clock.advance(time.Second)
	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{"listingId": listingB.String()}, authedCookies(), withCSRF("csrf-token"))
	var second conversationDTO
	decode(t, rec, &second)
	h.clock.advance(time.Second)
	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations/"+first.ConversationID+"/messages", allowedOrigin, map[string]string{"body": "later"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("send status = %d", rec.Code)
	}

	h.sessions.userID = seller
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodGet, "/v1/messaging/conversations", "", nil, authedCookies())
	var list conversationListDTO
	decode(t, rec, &list)
	if len(list.Conversations) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if list.Conversations[0].ConversationID != first.ConversationID {
		t.Fatalf("order = %+v", list.Conversations)
	}
	if list.Conversations[0].UnreadCount != 1 {
		t.Fatalf("unread = %+v", list.Conversations[0])
	}
	rec = do(t, h, http.MethodPost, "/v1/messaging/conversations/"+first.ConversationID+"/read", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("read status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/messaging/conversations", "", nil, authedCookies())
	decode(t, rec, &list)
	if list.Conversations[0].UnreadCount != 0 {
		t.Fatalf("after read = %+v", list.Conversations[0])
	}
}

func TestCreateRejectsClientSuppliedUserIDs(t *testing.T) {
	h := newTestHandler(t)
	listing := mustPublished(t, h)
	rec := do(t, h, http.MethodPost, "/v1/messaging/conversations", allowedOrigin, map[string]string{
		"listingId": listing.String(),
		"userId":    mustID(t).String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

type stubSessions struct {
	userID messaging.ID
	err    error
}

func (s stubSessions) Resolve(_ context.Context, rawToken string) (messaging.ID, error) {
	if s.err != nil {
		return messaging.ID{}, s.err
	}
	if rawToken == "" {
		return messaging.ID{}, ErrUnauthenticated
	}
	return s.userID, nil
}

type stubListings struct {
	refs map[listingcontracts.ID]listingcontracts.ListingRef
}

func (s *stubListings) setPublished(listingID, owner messaging.ID) {
	s.refs[listingcontracts.ID(listingID)] = listingcontracts.ListingRef{
		ID:          listingcontracts.ID(listingID),
		OwnerUserID: listingcontracts.ID(owner),
		Status:      listingcontracts.StatusPublished,
	}
}

func (s *stubListings) ResolveListingOwner(_ context.Context, listingID listingcontracts.ID) (listingcontracts.ListingRef, error) {
	ref, ok := s.refs[listingID]
	if !ok {
		return listingcontracts.ListingRef{}, listingcontracts.ErrNotFound
	}
	return ref, nil
}

func (s *stubListings) AssertListingOwnedBy(ctx context.Context, listingID, userID listingcontracts.ID) error {
	ref, err := s.ResolveListingOwner(ctx, listingID)
	if err != nil {
		return err
	}
	if ref.OwnerUserID != userID {
		return listingcontracts.ErrForbidden
	}
	return nil
}

type testClock struct {
	now time.Time
}

func (c *testClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
}

type testHandler struct {
	*Handler
	sessions stubSessions
	listings *stubListings
	clock    *testClock
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	user, err := messaging.NewID()
	if err != nil {
		t.Fatal(err)
	}
	store := messaging.NewMemoryStore()
	listings := &stubListings{refs: map[listingcontracts.ID]listingcontracts.ListingRef{}}
	clock := &testClock{now: time.Unix(50, 0).UTC()}
	svc, err := messaging.NewService(store, listings, func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	sessions := stubSessions{userID: user}
	h, err := New(sessions, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: h, sessions: sessions, listings: listings, clock: clock}
}

func mustPublished(t *testing.T, h *testHandler) messaging.ID {
	t.Helper()
	id := mustID(t)
	owner := mustID(t)
	h.listings.setPublished(id, owner)
	return id
}

func mustID(t *testing.T) messaging.ID {
	t.Helper()
	id, err := messaging.NewID()
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
