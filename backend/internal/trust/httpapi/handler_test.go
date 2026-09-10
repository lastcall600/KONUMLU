package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/identity"
	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/identity/publicprofile"
	"backend/internal/platform/outbox"
	reviewscontracts "backend/internal/reviews/contracts"
	"backend/internal/trust"
	verifiedcontracts "backend/internal/verified/contracts"
)

func TestMeRequiresSession(t *testing.T) {
	h := newTestHandler(t)
	rec := get(t, h, "/v1/trust/me", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestMeZeroState(t *testing.T) {
	h := newTestHandler(t)
	rec := get(t, h, "/v1/trust/me", authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body meDTO
	decode(t, rec, &body)
	if body.Level != trust.LevelNew || body.VerifiedInteractionCount != 0 ||
		body.RequesterVerifiedInteractionCount != 0 || body.ProviderVerifiedInteractionCount != 0 ||
		body.LastVerifiedInteractionAt != nil {
		t.Fatalf("body = %+v", body)
	}
	if body.VerifiedReviewCount != 0 || body.ProviderServiceReviewCount != 0 ||
		body.ProviderServiceAverage != nil || body.LastVerifiedReviewAt != nil {
		t.Fatalf("review zero body = %+v", body)
	}
}

func TestMePopulatedState(t *testing.T) {
	h := newTestHandler(t)
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	if err := h.handler.Handle(context.Background(), mustOutboxEvent(t, h.userID, mustID(t), at)); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/v1/trust/me", authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body meDTO
	decode(t, rec, &body)
	if body.Level != trust.LevelVerified || body.VerifiedInteractionCount != 1 ||
		body.RequesterVerifiedInteractionCount != 1 || body.ProviderVerifiedInteractionCount != 0 {
		t.Fatalf("body = %+v", body)
	}
	if body.LastVerifiedInteractionAt == nil || *body.LastVerifiedInteractionAt != at.Format(time.RFC3339) {
		t.Fatalf("last = %v", body.LastVerifiedInteractionAt)
	}
}

func TestMeTransactionAndDeliveryDoNotChangeLevel(t *testing.T) {
	h := newTestHandler(t)
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	if err := h.handler.Handle(context.Background(), mustTypedOutboxEvent(t, h.userID, mustID(t), at, verifiedcontracts.InteractionTypeTransaction)); err != nil {
		t.Fatal(err)
	}
	if err := h.handler.Handle(context.Background(), mustTypedOutboxEvent(t, h.userID, mustID(t), at.Add(time.Minute), verifiedcontracts.InteractionTypeDelivery)); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/v1/trust/me", authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body meDTO
	decode(t, rec, &body)
	if body.Level != trust.LevelNew || body.VerifiedInteractionCount != 0 ||
		body.RequesterVerifiedInteractionCount != 0 || body.ProviderVerifiedInteractionCount != 0 ||
		body.LastVerifiedInteractionAt != nil {
		t.Fatalf("transaction/delivery leaked into me DTO = %+v", body)
	}
}

func TestMePopulatedReviewState(t *testing.T) {
	h := newTestHandler(t)
	at := time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
	if err := h.handler.Handle(context.Background(), mustReviewOutboxEvent(t, h.userID, mustID(t), 5, 4, at)); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/v1/trust/me", authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body meDTO
	decode(t, rec, &body)
	if body.Level != trust.LevelNew || body.VerifiedInteractionCount != 0 {
		t.Fatalf("level mixed with reviews = %+v", body)
	}
	if body.VerifiedReviewCount != 1 || body.ProviderServiceReviewCount != 0 || body.ProviderServiceAverage != nil {
		t.Fatalf("reviewer body = %+v", body)
	}
	if body.LastVerifiedReviewAt == nil || *body.LastVerifiedReviewAt != at.Format(time.RFC3339) {
		t.Fatalf("lastVerifiedReviewAt = %v", body.LastVerifiedReviewAt)
	}

	provider := newTestHandler(t)
	if err := provider.handler.Handle(context.Background(), mustReviewOutboxEvent(t, mustID(t), provider.userID, 5, 4, at)); err != nil {
		t.Fatal(err)
	}
	rec = get(t, provider, "/v1/trust/me", authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("provider status = %d body=%s", rec.Code, rec.Body.String())
	}
	decode(t, rec, &body)
	if body.Level != trust.LevelNew || body.VerifiedReviewCount != 0 || body.ProviderServiceReviewCount != 1 {
		t.Fatalf("provider body = %+v", body)
	}
	if body.ProviderServiceAverage == nil || body.ProviderServiceAverage.String() != "4" {
		t.Fatalf("provider average = %v", body.ProviderServiceAverage)
	}
	if body.LastVerifiedReviewAt != nil {
		t.Fatalf("provider authored timestamp = %v", body.LastVerifiedReviewAt)
	}
}

func TestMeRejectsChosenUserID(t *testing.T) {
	h := newTestHandler(t)
	other := mustID(t)
	rec := get(t, h, "/v1/trust/me?userId="+other.String(), authedCookies())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("userId status = %d", rec.Code)
	}
	rec = get(t, h, "/v1/trust/me?user_id="+other.String(), authedCookies())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("user_id status = %d", rec.Code)
	}
}

func TestPublicTrustByPublicProfile(t *testing.T) {
	h := newTestHandler(t)
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	if err := h.handler.Handle(context.Background(), mustOutboxEvent(t, mustID(t), h.userID, at)); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/v1/public/profiles/"+h.publicID.String()+"/trust", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body publicDTO
	decode(t, rec, &body)
	if body.Level != trust.LevelVerified || body.VerifiedInteractionCount != 1 ||
		body.ProviderVerifiedInteractionCount != 1 {
		t.Fatalf("body = %+v", body)
	}
	if body.LastVerifiedInteractionAt == nil || *body.LastVerifiedInteractionAt != at.Format(time.RFC3339) {
		t.Fatalf("last = %v", body.LastVerifiedInteractionAt)
	}
	assertPublicPrivacy(t, rec.Body.String(), h.userID)
}

func TestPublicTrustZeroWhenProjectionMissing(t *testing.T) {
	h := newTestHandler(t)
	rec := get(t, h, "/v1/public/profiles/"+h.publicID.String()+"/trust", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body publicDTO
	decode(t, rec, &body)
	if body.Level != trust.LevelNew || body.VerifiedInteractionCount != 0 ||
		body.ProviderVerifiedInteractionCount != 0 || body.ProviderServiceReviewCount != 0 ||
		body.ProviderServiceAverage != nil || body.LastVerifiedInteractionAt != nil {
		t.Fatalf("body = %+v", body)
	}
	assertPublicPrivacy(t, rec.Body.String(), h.userID)
}

func TestPublicTrustMalformedPublicProfileID(t *testing.T) {
	h := newTestHandler(t)
	rec := get(t, h, "/v1/public/profiles/not-a-uuid/trust", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestPublicTrustMissingProfile(t *testing.T) {
	h := newTestHandler(t)
	missing := mustID(t)
	rec := get(t, h, "/v1/public/profiles/"+missing.String()+"/trust", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPublicTrustDisabledOrDeletedProfile(t *testing.T) {
	h := newTestHandler(t)
	h.profiles = stubProfiles{publicID: identityID(h.publicID), userID: identityID(h.userID), err: identitycontracts.ErrNotFound}
	rec := get(t, h, "/v1/public/profiles/"+h.publicID.String()+"/trust", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPublicTrustHiddenWhenIdentityProfileRestricted(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	userID := mustID(t)
	idUser := identity.User{ID: identity.ID(userID), CreatedAt: now, UpdatedAt: now}
	profileStore := publicprofile.NewMemoryStore()
	profileStore.PutUser(idUser)
	profileSvc, err := publicprofile.NewService(profileStore, func() time.Time { return now.Add(time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := publicprofile.NewResolver(profileSvc)
	if err != nil {
		t.Fatal(err)
	}
	me, err := profileSvc.GetMe(context.Background(), idUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	store := trust.NewMemoryStore()
	p, err := trust.NewProjector(store, trust.DefaultLevelPolicy())
	if err != nil {
		t.Fatal(err)
	}
	projection, err := trust.NewProjectionHandler(p)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	if err := projection.Handle(context.Background(), mustOutboxEvent(t, mustID(t), userID, at)); err != nil {
		t.Fatal(err)
	}
	svc, err := trust.NewService(p)
	if err != nil {
		t.Fatal(err)
	}
	httpHandler, err := New(stubSessions{userID: userID}, svc, resolver, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	h := &testHandler{Handler: httpHandler, handler: projection, userID: userID, publicID: trust.ID(me.PublicProfileID)}
	rec := get(t, h, "/v1/public/profiles/"+h.publicID.String()+"/trust", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("visible status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := profileSvc.ApplyModerationState(context.Background(), identitycontracts.ApplyPublicProfileModerationInput{
		PublicProfileID: identitycontracts.ID(me.PublicProfileID), State: identitycontracts.ModerationStateRestricted,
	}); err != nil {
		t.Fatal(err)
	}
	rec = get(t, h, "/v1/public/profiles/"+h.publicID.String()+"/trust", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("restricted status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertPublicPrivacy(t, rec.Body.String(), userID)
	rec = get(t, h, "/v1/trust/me", authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("self trust status = %d body=%s", rec.Code, rec.Body.String())
	}
	var meBody meDTO
	decode(t, rec, &meBody)
	if meBody.VerifiedInteractionCount != 1 {
		t.Fatalf("trust projection mutated = %+v", meBody)
	}
	if err := profileSvc.ApplyModerationState(context.Background(), identitycontracts.ApplyPublicProfileModerationInput{
		PublicProfileID: identitycontracts.ID(me.PublicProfileID), State: identitycontracts.ModerationStateRemoved,
	}); err != nil {
		t.Fatal(err)
	}
	rec = get(t, h, "/v1/public/profiles/"+h.publicID.String()+"/trust", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("removed status = %d", rec.Code)
	}
}

func TestPublicTrustOmitsPrivateFields(t *testing.T) {
	h := newTestHandler(t)
	at := time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
	if err := h.handler.Handle(context.Background(), mustReviewOutboxEvent(t, mustID(t), h.userID, 5, 4, at)); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/v1/public/profiles/"+h.publicID.String()+"/trust", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body publicDTO
	decode(t, rec, &body)
	if body.Level != trust.LevelNew {
		t.Fatalf("review average changed level: %+v", body)
	}
	if body.ProviderServiceReviewCount != 1 || body.ProviderServiceAverage == nil || body.ProviderServiceAverage.String() != "4" {
		t.Fatalf("provider service = %+v", body)
	}
	raw := rec.Body.String()
	assertPublicPrivacy(t, raw, h.userID)
	lower := strings.ToLower(raw)
	for _, leaked := range []string{
		"requesterverifiedinteractioncount",
		"verifiedreviewcount",
		"lastverifiedreviewat",
		"body",
		"comment",
		"history",
		"eventid",
		"reviewid",
	} {
		if strings.Contains(lower, leaked) {
			t.Fatalf("private field %q leaked: %s", leaked, raw)
		}
	}
}

func TestNoDirectUserUUIDPublicTrustEndpoint(t *testing.T) {
	h := newTestHandler(t)
	rec := get(t, h, "/v1/public/trust/"+h.userID.String(), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func assertPublicPrivacy(t *testing.T, raw string, userID trust.ID) {
	t.Helper()
	lower := strings.ToLower(raw)
	if strings.Contains(lower, `"userid"`) || strings.Contains(lower, "user_id") || strings.Contains(raw, userID.String()) {
		t.Fatalf("user id leaked: %s", raw)
	}
}

type testHandler struct {
	*Handler
	handler  *trust.ProjectionHandler
	userID   trust.ID
	publicID trust.ID
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	userID := mustID(t)
	store := trust.NewMemoryStore()
	p, err := trust.NewProjector(store, trust.DefaultLevelPolicy())
	if err != nil {
		t.Fatal(err)
	}
	h, err := trust.NewProjectionHandler(p)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := trust.NewService(p)
	if err != nil {
		t.Fatal(err)
	}
	publicID := mustID(t)
	httpHandler, err := New(stubSessions{userID: userID}, svc, stubProfiles{
		publicID: identityID(publicID),
		userID:   identityID(userID),
	}, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: httpHandler, handler: h, userID: userID, publicID: publicID}
}

func identityID(id trust.ID) identitycontracts.ID {
	return identitycontracts.ID(id)
}

type stubProfiles struct {
	publicID identitycontracts.ID
	userID   identitycontracts.ID
	err      error
}

func (s stubProfiles) ResolveByPublicID(_ context.Context, publicProfileID identitycontracts.ID) (identitycontracts.PublicProfile, error) {
	if s.err != nil {
		return identitycontracts.PublicProfile{}, s.err
	}
	if publicProfileID != s.publicID {
		return identitycontracts.PublicProfile{}, identitycontracts.ErrNotFound
	}
	return identitycontracts.PublicProfile{
		PublicProfileID: publicProfileID,
		MemberSince:     time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (s stubProfiles) ResolveByUserID(_ context.Context, userID identitycontracts.ID) (identitycontracts.PublicProfile, error) {
	if s.err != nil {
		return identitycontracts.PublicProfile{}, s.err
	}
	if userID != s.userID {
		return identitycontracts.PublicProfile{}, identitycontracts.ErrNotFound
	}
	return identitycontracts.PublicProfile{
		PublicProfileID: s.publicID,
		MemberSince:     time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (s stubProfiles) ResolveUserIDByPublicID(_ context.Context, publicProfileID identitycontracts.ID) (identitycontracts.ID, error) {
	if s.err != nil {
		return identitycontracts.ID{}, s.err
	}
	if publicProfileID != s.publicID {
		return identitycontracts.ID{}, identitycontracts.ErrNotFound
	}
	return s.userID, nil
}

type stubSessions struct {
	userID trust.ID
	err    error
}

func (s stubSessions) Resolve(_ context.Context, rawToken string) (trust.ID, error) {
	if s.err != nil {
		return trust.ID{}, s.err
	}
	if rawToken == "" {
		return trust.ID{}, ErrUnauthenticated
	}
	return s.userID, nil
}

func mustOutboxEvent(t *testing.T, requester, provider trust.ID, at time.Time) outbox.Event {
	t.Helper()
	return mustTypedOutboxEvent(t, requester, provider, at, verifiedcontracts.InteractionTypeListingInspection)
}

func mustTypedOutboxEvent(t *testing.T, requester, provider trust.ID, at time.Time, interactionType string) outbox.Event {
	t.Helper()
	interaction, listing := mustID(t), mustID(t)
	payloadBody := verifiedcontracts.InteractionCompletedPayload{
		InteractionID:      interaction.String(),
		ListingID:          listing.String(),
		RequesterUserID:    requester.String(),
		ProviderUserID:     provider.String(),
		InteractionType:    interactionType,
		VerificationMethod: "otp",
		VerifiedAt:         at.UTC().Format(time.RFC3339),
	}
	switch interactionType {
	case verifiedcontracts.InteractionTypeListingInspection:
		payloadBody.AppointmentID = mustID(t).String()
	case verifiedcontracts.InteractionTypeTransaction, verifiedcontracts.InteractionTypeDelivery:
		payloadBody.FlowID = mustID(t).String()
	}
	payload, err := json.Marshal(payloadBody)
	if err != nil {
		t.Fatal(err)
	}
	eid, err := outbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return outbox.Event{
		ID:           eid,
		EventType:    verifiedcontracts.EventTypeInteractionCompleted,
		EventVersion: verifiedcontracts.EventVersion,
		Payload:      payload,
		CreatedAt:    at,
	}
}

func mustReviewOutboxEvent(t *testing.T, reviewer, provider trust.ID, listingAccuracy, providerService int, at time.Time) outbox.Event {
	t.Helper()
	payload, err := json.Marshal(reviewscontracts.VerifiedCreatedPayload{
		ReviewID:              mustID(t).String(),
		VerifiedInteractionID: mustID(t).String(),
		ListingID:             mustID(t).String(),
		ReviewerUserID:        reviewer.String(),
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

func mustID(t *testing.T) trust.ID {
	t.Helper()
	id, err := trust.NewID()
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
