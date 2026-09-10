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
	"backend/internal/platform/outbox"
	"backend/internal/reviews"
	verifiedcontracts "backend/internal/verified/contracts"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)
	interaction := h.putInspection(t, h.sessions.userID)
	body := validCreate(interaction)

	rec := do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, body, nil, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/reviews", "", body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, body, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestReadsRequireSessionAndAreSessionBound(t *testing.T) {
	h := newTestHandler(t)
	interaction := h.putInspection(t, h.sessions.userID)
	rec := do(t, h, http.MethodGet, "/v1/reviews/mine", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("mine status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/v1/reviews/eligibility/"+interaction.String(), "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("eligibility status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodGet, "/v1/reviews/eligibility/"+interaction.String(), "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("owner eligibility status = %d body=%s", rec.Code, rec.Body.String())
	}
	var elig eligibilityDTO
	decode(t, rec, &elig)
	if !elig.Eligible || elig.AlreadyReviewed || elig.ExpiresAt == nil {
		t.Fatalf("eligibility = %+v", elig)
	}

	h.sessions.userID = mustID(t)
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodGet, "/v1/reviews/eligibility/"+interaction.String(), "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign eligibility status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
}

func TestCreateCompletedInteractionAndSeparateRatings(t *testing.T) {
	h := newTestHandler(t)
	interaction := h.putInspection(t, h.sessions.userID)
	rec := do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, map[string]any{
		"verifiedInteractionId": interaction.String(),
		"listingAccuracy":       5,
		"providerService":       1,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created reviewDTO
	decode(t, rec, &created)
	if created.ListingAccuracy != 5 || created.ProviderService != 1 {
		t.Fatalf("ratings = %+v", created)
	}

	rec = do(t, h, http.MethodGet, "/v1/reviews/mine", "", nil, authedCookies())
	var list reviewListDTO
	decode(t, rec, &list)
	if len(list.Reviews) != 1 || list.Reviews[0].ListingAccuracy != 5 || list.Reviews[0].ProviderService != 1 {
		t.Fatalf("mine = %+v", list)
	}
}

func TestWrongRequesterCreateIsNotFound(t *testing.T) {
	h := newTestHandler(t)
	owner := h.sessions.userID
	interaction := h.putInspection(t, owner)
	h.sessions.userID = mustID(t)
	h.Handler.sessions = h.sessions
	rec := do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, validCreate(interaction), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExpiredWindowRejected(t *testing.T) {
	h := newTestHandler(t)
	interaction := h.putInspectionAt(t, h.sessions.userID, h.clock.now.Add(-25*time.Hour))
	rec := do(t, h, http.MethodGet, "/v1/reviews/eligibility/"+interaction.String(), "", nil, authedCookies())
	var elig eligibilityDTO
	decode(t, rec, &elig)
	if elig.Eligible {
		t.Fatalf("expired eligible: %+v", elig)
	}
	rec = do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, validCreate(interaction), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expired create status = %d", rec.Code)
	}
}

func TestDuplicateReviewConflictAndNoSecondEvent(t *testing.T) {
	h := newTestHandler(t)
	interaction := h.putInspection(t, h.sessions.userID)
	body := validCreate(interaction)
	rec := do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(h.outbox.Events) != 1 {
		t.Fatalf("events = %d", len(h.outbox.Events))
	}
	if strings.Contains(string(h.outbox.Events[0].Payload), "body") {
		t.Fatalf("body in event: %s", h.outbox.Events[0].Payload)
	}
}

func TestInvalidRatingAndBody(t *testing.T) {
	h := newTestHandler(t)
	interaction := h.putInspection(t, h.sessions.userID)
	rec := do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, map[string]any{
		"verifiedInteractionId": interaction.String(),
		"listingAccuracy":       0,
		"providerService":       3,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rating status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, map[string]any{
		"verifiedInteractionId": interaction.String(),
		"body":                  strings.Repeat("a", reviews.MaxBodyBytes+1),
		"listingAccuracy":       3,
		"providerService":       3,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversize status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, map[string]any{
		"verifiedInteractionId": interaction.String(),
		"listingAccuracy":       2,
		"providerService":       4,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("optional body status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPublicListingReturnsVerifiedReviewsNewestFirst(t *testing.T) {
	h := newTestHandler(t)
	listing := mustID(t)
	h.listings.publish(listing)
	older := h.putInspectionOn(t, h.sessions.userID, listing, h.clock.now.Add(-2*time.Hour))
	rec := do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, map[string]any{
		"verifiedInteractionId": older.String(),
		"body":                  "older",
		"listingAccuracy":       2,
		"providerService":       3,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("older create status = %d body=%s", rec.Code, rec.Body.String())
	}
	h.clock.now = h.clock.now.Add(time.Hour)
	newer := h.putInspectionOn(t, h.sessions.userID, listing, h.clock.now.Add(-time.Hour))
	rec = do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, map[string]any{
		"verifiedInteractionId": newer.String(),
		"body":                  "newer",
		"listingAccuracy":       5,
		"providerService":       4,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("newer create status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodGet, "/v1/public/listings/"+listing.String()+"/reviews", "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	rawBytes := rec.Body.Bytes()
	var list publicReviewListDTO
	decode(t, rec, &list)
	if len(list.Reviews) != 2 {
		t.Fatalf("reviews = %+v", list)
	}
	if list.Reviews[0].Body == nil || *list.Reviews[0].Body != "newer" {
		t.Fatalf("newest first = %+v", list.Reviews)
	}
	if list.Reviews[1].Body == nil || *list.Reviews[1].Body != "older" {
		t.Fatalf("older second = %+v", list.Reviews)
	}

	var raw map[string]any
	if err := json.Unmarshal(rawBytes, &raw); err != nil {
		t.Fatal(err)
	}
	rows, ok := raw["reviews"].([]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("raw reviews = %#v", raw)
	}
	first, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("first = %#v", rows[0])
	}
	for _, key := range []string{"reviewerUserId", "reviewer_user_id", "providerUserId", "provider_user_id", "verifiedInteractionId", "verified_interaction_id"} {
		if _, present := first[key]; present {
			t.Fatalf("leaked %s: %#v", key, first)
		}
	}
	if _, present := raw["nextCursor"]; present {
		t.Fatalf("unexpected nextCursor: %#v", raw)
	}
}

func TestPublicListingNonPublishedIsPrivacySafeNotFound(t *testing.T) {
	h := newTestHandler(t)
	listing := mustID(t)
	h.listings.rows[listingcontracts.ID(listing)] = listingcontracts.ListingRef{
		ID:          listingcontracts.ID(listing),
		OwnerUserID: listingcontracts.ID(mustID(t)),
		Status:      "archived",
	}
	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+listing.String()+"/reviews", "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
}

func TestPublicListingMissingIsPrivacySafeNotFound(t *testing.T) {
	h := newTestHandler(t)
	listing := mustID(t)
	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+listing.String()+"/reviews", "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
}

func TestPublicListingEmptyPublished(t *testing.T) {
	h := newTestHandler(t)
	listing := mustID(t)
	h.listings.publish(listing)
	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+listing.String()+"/reviews", "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list publicReviewListDTO
	decode(t, rec, &list)
	if list.Reviews == nil || len(list.Reviews) != 0 {
		t.Fatalf("list = %+v", list)
	}
}

func TestPublicListingMalformedUUID(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/public/listings/not-a-uuid/reviews", "", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
}

func TestPublicListingStoreUnavailable(t *testing.T) {
	h := newTestHandler(t)
	listing := mustID(t)
	h.listings.publish(listing)
	h.store.SetFail(reviews.ErrUnavailable)
	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+listing.String()+"/reviews", "", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
}

func TestPublicListingCursorPagesAndInvalidCursor(t *testing.T) {
	h := newTestHandler(t)
	listing := mustID(t)
	h.listings.publish(listing)
	bodies := []string{"a", "b", "c"}
	for i, body := range bodies {
		h.clock.now = h.clock.now.Add(time.Duration(i+1) * time.Hour)
		interaction := h.putInspectionOn(t, h.sessions.userID, listing, h.clock.now.Add(-time.Minute))
		rec := do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, map[string]any{
			"verifiedInteractionId": interaction.String(),
			"body":                  body,
			"listingAccuracy":       3,
			"providerService":       3,
		}, authedCookies(), withCSRF("csrf-token"))
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s status = %d body=%s", body, rec.Code, rec.Body.String())
		}
	}

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+listing.String()+"/reviews?limit=2", "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("page1 status = %d body=%s", rec.Code, rec.Body.String())
	}
	var page1 publicReviewListDTO
	decode(t, rec, &page1)
	if page1.NextCursor == nil || len(page1.Reviews) != 2 {
		t.Fatalf("page1 = %+v", page1)
	}
	if page1.Reviews[0].Body == nil || *page1.Reviews[0].Body != "c" {
		t.Fatalf("newest first = %+v", page1.Reviews)
	}

	rec = do(t, h, http.MethodGet, "/v1/public/listings/"+listing.String()+"/reviews?limit=2&cursor="+*page1.NextCursor, "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("page2 status = %d body=%s", rec.Code, rec.Body.String())
	}
	var page2 publicReviewListDTO
	decode(t, rec, &page2)
	if len(page2.Reviews) != 1 || page2.NextCursor != nil {
		t.Fatalf("page2 = %+v", page2)
	}
	if page2.Reviews[0].Body == nil || *page2.Reviews[0].Body != "a" {
		t.Fatalf("page2 body = %+v", page2.Reviews)
	}

	rec = do(t, h, http.MethodGet, "/v1/public/listings/"+listing.String()+"/reviews?cursor=not-a-cursor", "", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")

	rec = do(t, h, http.MethodGet, "/v1/public/listings/"+listing.String()+"/reviews?limit=99", "", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("limit status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
}

func TestCreateRejectsClientSuppliedUserIDs(t *testing.T) {
	h := newTestHandler(t)
	interaction := h.putInspection(t, h.sessions.userID)
	rec := do(t, h, http.MethodPost, "/v1/reviews", allowedOrigin, map[string]any{
		"verifiedInteractionId": interaction.String(),
		"listingAccuracy":       3,
		"providerService":       3,
		"reviewerUserId":        mustID(t).String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

type stubSessions struct {
	userID reviews.ID
	err    error
}

func (s stubSessions) Resolve(_ context.Context, rawToken string) (reviews.ID, error) {
	if s.err != nil {
		return reviews.ID{}, s.err
	}
	if rawToken == "" {
		return reviews.ID{}, ErrUnauthenticated
	}
	return s.userID, nil
}

type stubVerified struct {
	rows map[verifiedcontracts.ID]verifiedcontracts.InteractionRef
}

func (s *stubVerified) GetInteraction(_ context.Context, id verifiedcontracts.ID) (verifiedcontracts.InteractionRef, error) {
	row, ok := s.rows[id]
	if !ok {
		return verifiedcontracts.InteractionRef{}, verifiedcontracts.ErrNotFound
	}
	return row, nil
}

type memoryEnqueuer struct {
	Events []outbox.Event
}

func (e *memoryEnqueuer) Enqueue(ctx context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	if err := ctx.Err(); err != nil {
		return outbox.Event{}, err
	}
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	ev := outbox.Event{
		ID:           id,
		EventType:    in.EventType,
		EventVersion: in.EventVersion,
		Payload:      append([]byte(nil), in.Payload...),
		CreatedAt:    time.Now().UTC(),
		AvailableAt:  time.Now().UTC(),
	}
	e.Events = append(e.Events, ev)
	return ev, nil
}

type testClock struct {
	now time.Time
}

type stubListings struct {
	rows map[listingcontracts.ID]listingcontracts.ListingRef
}

func (s *stubListings) publish(listingID reviews.ID) {
	s.rows[listingcontracts.ID(listingID)] = listingcontracts.ListingRef{
		ID:          listingcontracts.ID(listingID),
		OwnerUserID: listingcontracts.ID(listingID),
		Status:      listingcontracts.StatusPublished,
	}
}

func (s *stubListings) ResolveListingOwner(_ context.Context, listingID listingcontracts.ID) (listingcontracts.ListingRef, error) {
	row, ok := s.rows[listingID]
	if !ok {
		return listingcontracts.ListingRef{}, listingcontracts.ErrNotFound
	}
	return row, nil
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

type testHandler struct {
	*Handler
	sessions stubSessions
	verified *stubVerified
	listings *stubListings
	store    *reviews.MemoryStore
	outbox   *memoryEnqueuer
	clock    *testClock
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	user, err := reviews.NewID()
	if err != nil {
		t.Fatal(err)
	}
	store := reviews.NewMemoryStore()
	verified := &stubVerified{rows: map[verifiedcontracts.ID]verifiedcontracts.InteractionRef{}}
	listings := &stubListings{rows: map[listingcontracts.ID]listingcontracts.ListingRef{}}
	enqueuer := &memoryEnqueuer{}
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	svc, err := reviews.NewService(store, verified, listings, enqueuer, reviews.DefaultPolicy(), func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	sessions := stubSessions{userID: user}
	h, err := New(sessions, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: h, sessions: sessions, verified: verified, listings: listings, store: store, outbox: enqueuer, clock: clock}
}

func (h *testHandler) putInspection(t *testing.T, requester reviews.ID) reviews.ID {
	t.Helper()
	return h.putInspectionAt(t, requester, h.clock.now.Add(-time.Hour))
}

func (h *testHandler) putInspectionAt(t *testing.T, requester reviews.ID, verifiedAt time.Time) reviews.ID {
	t.Helper()
	return h.putInspectionOn(t, requester, mustID(t), verifiedAt)
}

func (h *testHandler) putInspectionOn(t *testing.T, requester, listing reviews.ID, verifiedAt time.Time) reviews.ID {
	t.Helper()
	id := mustID(t)
	h.verified.rows[verifiedcontracts.ID(id)] = verifiedcontracts.InteractionRef{
		ID:              verifiedcontracts.ID(id),
		ListingID:       verifiedcontracts.ID(listing),
		RequesterUserID: verifiedcontracts.ID(requester),
		ProviderUserID:  verifiedcontracts.ID(mustID(t)),
		InteractionType: verifiedcontracts.InteractionTypeListingInspection,
		VerifiedAt:      verifiedAt,
	}
	return id
}

func validCreate(interaction reviews.ID) map[string]any {
	return map[string]any{
		"verifiedInteractionId": interaction.String(),
		"listingAccuracy":       4,
		"providerService":       3,
	}
}

func mustID(t *testing.T) reviews.ID {
	t.Helper()
	id, err := reviews.NewID()
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
