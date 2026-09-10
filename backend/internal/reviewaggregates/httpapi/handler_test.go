package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/platform/outbox"
	"backend/internal/reviewaggregates"
	reviewscontracts "backend/internal/reviews/contracts"
)

func TestPublicListingZeroState(t *testing.T) {
	h := newTestHandler(t)
	listing := mustID(t)
	rec := get(t, h, "/v1/public/listings/"+listing.String()+"/review-summary", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body listingSummaryDTO
	decode(t, rec, &body)
	if body.ListingAccuracy.ReviewCount != 0 || body.ListingAccuracy.Average != nil {
		t.Fatalf("body = %+v", body)
	}
}

func TestPublicListingPopulatedState(t *testing.T) {
	h := newTestHandler(t)
	listing := mustID(t)
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	if err := h.handler.Handle(context.Background(), mustOutboxEvent(t, listing, mustID(t), 4, 1, at)); err != nil {
		t.Fatal(err)
	}
	if err := h.handler.Handle(context.Background(), mustOutboxEvent(t, listing, mustID(t), 5, 2, at.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/v1/public/listings/"+listing.String()+"/review-summary", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body listingSummaryDTO
	decode(t, rec, &body)
	if body.ListingAccuracy.ReviewCount != 2 {
		t.Fatalf("count = %d", body.ListingAccuracy.ReviewCount)
	}
	if body.ListingAccuracy.Average == nil || body.ListingAccuracy.Average.String() != "4.5" {
		t.Fatalf("average = %v", body.ListingAccuracy.Average)
	}
}

func TestSelfProviderZeroState(t *testing.T) {
	h := newTestHandler(t)
	rec := get(t, h, "/v1/review-summary/me", authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body meSummaryDTO
	decode(t, rec, &body)
	if body.ProviderService.ReviewCount != 0 || body.ProviderService.Average != nil {
		t.Fatalf("body = %+v", body)
	}
}

func TestSelfProviderPopulatedState(t *testing.T) {
	h := newTestHandler(t)
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	if err := h.handler.Handle(context.Background(), mustOutboxEvent(t, mustID(t), h.userID, 5, 3, at)); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/v1/review-summary/me", authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body meSummaryDTO
	decode(t, rec, &body)
	if body.ProviderService.ReviewCount != 1 {
		t.Fatalf("count = %d", body.ProviderService.ReviewCount)
	}
	if body.ProviderService.Average == nil || body.ProviderService.Average.String() != "3" {
		t.Fatalf("average = %v", body.ProviderService.Average)
	}
}

func TestSelfRequiresSession(t *testing.T) {
	h := newTestHandler(t)
	rec := get(t, h, "/v1/review-summary/me", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

type testHandler struct {
	*Handler
	handler *reviewaggregates.ProjectionHandler
	userID  reviewaggregates.ID
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	userID := mustID(t)
	store := reviewaggregates.NewMemoryStore()
	p, err := reviewaggregates.NewProjector(store)
	if err != nil {
		t.Fatal(err)
	}
	h, err := reviewaggregates.NewProjectionHandler(p)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := reviewaggregates.NewService(p)
	if err != nil {
		t.Fatal(err)
	}
	httpHandler, err := New(stubSessions{userID: userID}, svc, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: httpHandler, handler: h, userID: userID}
}

type stubSessions struct {
	userID reviewaggregates.ID
	err    error
}

func (s stubSessions) Resolve(_ context.Context, rawToken string) (reviewaggregates.ID, error) {
	if s.err != nil {
		return reviewaggregates.ID{}, s.err
	}
	if rawToken == "" {
		return reviewaggregates.ID{}, ErrUnauthenticated
	}
	return s.userID, nil
}

func mustOutboxEvent(t *testing.T, listing, provider reviewaggregates.ID, listingAccuracy, providerService int, at time.Time) outbox.Event {
	t.Helper()
	payload, err := json.Marshal(reviewscontracts.VerifiedCreatedPayload{
		ReviewID:              mustID(t).String(),
		VerifiedInteractionID: mustID(t).String(),
		ListingID:             listing.String(),
		ReviewerUserID:        mustID(t).String(),
		ProviderUserID:        provider.String(),
		ListingAccuracy:       listingAccuracy,
		ProviderService:       providerService,
		CreatedAt:             at.UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	eid, err := outbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return outbox.Event{
		ID:           eid,
		EventType:    reviewscontracts.EventTypeVerifiedCreated,
		EventVersion: reviewscontracts.EventVersion,
		Payload:      payload,
		CreatedAt:    at,
	}
}

func mustID(t *testing.T) reviewaggregates.ID {
	t.Helper()
	id, err := reviewaggregates.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func get(t *testing.T, h *testHandler, path string, cookies map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range cookies {
		r.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(rec, r)
	return rec
}

func authedCookies() map[string]string {
	return map[string]string{sessionCookieName: "session-token"}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dest); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
}
