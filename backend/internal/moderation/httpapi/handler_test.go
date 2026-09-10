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

	identitycontracts "backend/internal/identity/contracts"
	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/moderation"
	"backend/internal/platform/outbox"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.other)
	body := validListingReport(listing)

	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, body, nil, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/moderation/reports", "", body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, body, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestCreateListingAndPublicProfileReports(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.other)
	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, validListingReport(listing), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("listing status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created reportDTO
	decode(t, rec, &created)
	if created.TargetType != "listing" || created.Status != "submitted" || created.TargetID != listing.String() {
		t.Fatalf("created = %+v", created)
	}

	profile := h.putProfile(t, h.other)
	rec = do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, map[string]any{
		"targetType": "public_profile",
		"targetId":   profile.String(),
		"reasonCode": "impersonation",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("profile status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestInvalidTargetReasonAndDescription(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.other)
	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, map[string]any{
		"targetType": "listing",
		"targetId":   listing.String(),
		"reasonCode": "made_up",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reason status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "bad_request")

	rec = do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, map[string]any{
		"targetType": "listing",
		"targetId":   mustID(t).String(),
		"reasonCode": "spam",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")

	rec = do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, map[string]any{
		"targetType":  "listing",
		"targetId":    listing.String(),
		"reasonCode":  "spam",
		"description": strings.Repeat("a", moderation.MaxDescriptionBytes+1),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversize status = %d", rec.Code)
	}
}

func TestReporterSpoofRejected(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.other)
	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, map[string]any{
		"targetType":     "listing",
		"targetId":       listing.String(),
		"reasonCode":     "spam",
		"reporterUserId": mustID(t).String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
}

func TestDuplicateHandlingConflict(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.other)
	body := validListingReport(listing)
	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "conflict")
}

func TestMineListOwnershipAndPrivacy(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.other)
	rec := do(t, h, http.MethodGet, "/v1/moderation/reports/mine", "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth mine status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, validListingReport(listing), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodGet, "/v1/moderation/reports/mine", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("mine status = %d body=%s", rec.Code, rec.Body.String())
	}
	rawBytes := rec.Body.Bytes()
	var list reportListDTO
	decode(t, rec, &list)
	if len(list.Reports) != 1 {
		t.Fatalf("mine = %+v", list)
	}
	var raw map[string]any
	if err := json.Unmarshal(rawBytes, &raw); err != nil {
		t.Fatal(err)
	}
	rows, ok := raw["reports"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("raw = %#v", raw)
	}
	first, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("first = %#v", rows[0])
	}
	for _, key := range []string{
		"reporterUserId", "reporter_user_id", "assignedToId", "decision",
		"targetOwnerUserId", "ownerUserId", "staffNote", "staff_note", "statusChangedBy",
	} {
		if _, present := first[key]; present {
			t.Fatalf("leaked %s: %#v", key, first)
		}
	}

	h.sessions.userID = h.other
	h.Handler.sessions = h.sessions
	rec = do(t, h, http.MethodGet, "/v1/moderation/reports/mine", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("other mine status = %d body=%s", rec.Code, rec.Body.String())
	}
	var other reportListDTO
	decode(t, rec, &other)
	if len(other.Reports) != 0 {
		t.Fatalf("unrelated user saw reports: %+v", other)
	}
}

func TestSelfReportIsForbidden(t *testing.T) {
	h := newTestHandler(t)
	own := h.publishListing(t, h.sessions.userID)
	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, validListingReport(own), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "forbidden")
}

type stubSessions struct {
	userID moderation.ID
	err    error
}

func (s stubSessions) Resolve(_ context.Context, rawToken string) (moderation.ID, error) {
	if s.err != nil {
		return moderation.ID{}, s.err
	}
	if rawToken == "" {
		return moderation.ID{}, ErrUnauthenticated
	}
	return s.userID, nil
}

type stubListings struct {
	rows map[listingcontracts.ID]listingcontracts.ListingRef
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

type stubProfiles struct {
	byPublic map[identitycontracts.ID]identitycontracts.ID
}

func (s *stubProfiles) ResolveByPublicID(_ context.Context, publicProfileID identitycontracts.ID) (identitycontracts.PublicProfile, error) {
	if _, ok := s.byPublic[publicProfileID]; !ok {
		return identitycontracts.PublicProfile{}, identitycontracts.ErrNotFound
	}
	return identitycontracts.PublicProfile{PublicProfileID: publicProfileID, MemberSince: time.Unix(1, 0).UTC()}, nil
}

func (s *stubProfiles) ResolveByUserID(_ context.Context, userID identitycontracts.ID) (identitycontracts.PublicProfile, error) {
	return identitycontracts.PublicProfile{}, identitycontracts.ErrNotFound
}

func (s *stubProfiles) ResolveUserIDByPublicID(_ context.Context, publicProfileID identitycontracts.ID) (identitycontracts.ID, error) {
	owner, ok := s.byPublic[publicProfileID]
	if !ok {
		return identitycontracts.ID{}, identitycontracts.ErrNotFound
	}
	return owner, nil
}

type testClock struct {
	now time.Time
}

type testHandler struct {
	*Handler
	sessions stubSessions
	listings *stubListings
	profiles *stubProfiles
	other    moderation.ID
	clock    *testClock
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	user, err := moderation.NewID()
	if err != nil {
		t.Fatal(err)
	}
	other, err := moderation.NewID()
	if err != nil {
		t.Fatal(err)
	}
	listings := &stubListings{rows: map[listingcontracts.ID]listingcontracts.ListingRef{}}
	profiles := &stubProfiles{byPublic: map[identitycontracts.ID]identitycontracts.ID{}}
	clock := &testClock{now: time.Unix(1_700_000_000, 0).UTC()}
	svc, err := moderation.NewService(moderation.NewMemoryStore(), listings, profiles, moderation.DefaultPolicy(), func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	svc.SetOutbox(&staffMemoryOutbox{keys: make(map[string]struct{})})
	sessions := stubSessions{userID: user}
	h, err := New(sessions, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: h, sessions: sessions, listings: listings, profiles: profiles, other: other, clock: clock}
}

func (h *testHandler) publishListing(t *testing.T, owner moderation.ID) moderation.ID {
	t.Helper()
	id := mustID(t)
	h.listings.rows[listingcontracts.ID(id)] = listingcontracts.ListingRef{
		ID:          listingcontracts.ID(id),
		OwnerUserID: listingcontracts.ID(owner),
		Status:      listingcontracts.StatusPublished,
	}
	return id
}

func (h *testHandler) putProfile(t *testing.T, owner moderation.ID) moderation.ID {
	t.Helper()
	id := mustID(t)
	h.profiles.byPublic[identitycontracts.ID(id)] = identitycontracts.ID(owner)
	return id
}

func validListingReport(listing moderation.ID) map[string]any {
	return map[string]any{
		"targetType": "listing",
		"targetId":   listing.String(),
		"reasonCode": "spam",
	}
}

func mustID(t *testing.T) moderation.ID {
	t.Helper()
	id, err := moderation.NewID()
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
	if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(dest); err != nil {
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

type staffMemoryOutbox struct {
	keys map[string]struct{}
}

func (e *staffMemoryOutbox) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	if e.keys == nil {
		e.keys = make(map[string]struct{})
	}
	if in.IdempotencyKey != "" {
		if _, ok := e.keys[in.IdempotencyKey]; ok {
			return outbox.Event{}, outbox.ErrConflict
		}
		e.keys[in.IdempotencyKey] = struct{}{}
	}
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	return outbox.Event{ID: id, EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
}
