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

	"backend/internal/businesses"
	"backend/internal/needs"
	"backend/internal/offers"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newFixture(t)
	need := h.openNeed()
	svc := h.activeService(asBiz(h.provider), businesses.Coordinates{Latitude: 36.624, Longitude: 29.116})
	body := createBody(svc)

	rec := do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", allowedOrigin, body, map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}

	h.sessions.userID = h.provider
	rec = do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", "", body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", allowedOrigin, body, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestEligibleProviderSubmitsAndSpoofRejected(t *testing.T) {
	h := newFixture(t)
	need := h.openNeed()
	svc := h.activeService(asBiz(h.provider), businesses.Coordinates{Latitude: 36.624, Longitude: 29.116})
	h.sessions.userID = h.provider
	rec := do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", allowedOrigin, createBody(svc), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto providerOfferDTO
	decode(t, rec, &dto)
	if dto.Status != string(offers.StatusSubmitted) || dto.Need == nil || dto.Need.Title == "" {
		t.Fatalf("dto = %+v", dto)
	}
	assertNoUserIDs(t, rec.Body.String(), h.provider.String(), h.requester.String())

	body := createBody(svc)
	body["providerUserId"] = mustOfferID(t).String()
	rec = do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("spoof status = %d", rec.Code)
	}
}

func TestNonCandidateAndInactiveDenied(t *testing.T) {
	h := newFixture(t)
	need := h.openNeed()
	far := h.activeService(asBiz(h.provider), businesses.Coordinates{Latitude: 36.85, Longitude: 29.116})
	h.sessions.userID = h.provider
	rec := do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", allowedOrigin, createBody(far), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("far status = %d body=%s", rec.Code, rec.Body.String())
	}

	owner := mustBizUser(t)
	paused := h.activeService(owner, businesses.Coordinates{Latitude: 36.623, Longitude: 29.116})
	if _, err := h.biz.PauseOfferedService(context.Background(), owner, paused.BusinessID, paused.ID); err != nil {
		t.Fatal(err)
	}
	h.sessions.userID = offers.ID(owner)
	rec = do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", allowedOrigin, createBody(paused), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("paused status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequesterSeesOffersUnrelatedDenied(t *testing.T) {
	h := newFixture(t)
	need := h.openNeed()
	svc := h.activeService(asBiz(h.provider), businesses.Coordinates{Latitude: 36.624, Longitude: 29.116})
	h.sessions.userID = h.provider
	rec := do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", allowedOrigin, createBody(svc), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.userID = h.requester
	rec = do(t, h, http.MethodGet, "/v1/needs/"+need.ID.String()+"/offers", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var listed requesterListDTO
	decode(t, rec, &listed)
	if len(listed.Offers) != 1 || listed.Offers[0].BusinessDisplayName == "" {
		t.Fatalf("listed = %+v", listed)
	}
	assertNoUserIDs(t, rec.Body.String(), h.provider.String(), h.requester.String())

	h.sessions.userID = mustOfferID(t)
	rec = do(t, h, http.MethodGet, "/v1/needs/"+need.ID.String()+"/offers", "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign list status = %d", rec.Code)
	}
}

func TestWithdrawAcceptRejectAndTerminal(t *testing.T) {
	h := newFixture(t)
	need := h.openNeed()
	firstSvc := h.activeService(asBiz(h.provider), businesses.Coordinates{Latitude: 36.623, Longitude: 29.116})
	secondOwner := mustBizUser(t)
	secondSvc := h.activeService(secondOwner, businesses.Coordinates{Latitude: 36.624, Longitude: 29.116})

	h.sessions.userID = h.provider
	rec := do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", allowedOrigin, createBody(firstSvc), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var first providerOfferDTO
	decode(t, rec, &first)

	h.sessions.userID = offers.ID(secondOwner)
	rec = do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", allowedOrigin, createBody(secondSvc), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var second providerOfferDTO
	decode(t, rec, &second)

	rec = do(t, h, http.MethodPost, "/v1/offers/"+second.OfferID+"/withdraw", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw status = %d body=%s", rec.Code, rec.Body.String())
	}

	h.sessions.userID = h.requester
	rec = do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers/"+first.OfferID+"/accept", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept status = %d body=%s", rec.Code, rec.Body.String())
	}
	var accepted requesterOfferDTO
	decode(t, rec, &accepted)
	if accepted.Status != string(offers.StatusAccepted) {
		t.Fatalf("accepted = %+v", accepted)
	}
	rec = do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers/"+first.OfferID+"/reject", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("terminal reject status = %d", rec.Code)
	}

	owned, err := h.needSvc.GetOwned(context.Background(), needs.ID(h.requester), needs.ID(need.ID))
	if err != nil || owned.Status != needs.StatusOpen {
		t.Fatalf("need after accept = %+v err = %v", owned, err)
	}
}

func TestProviderNeedViewAndMine(t *testing.T) {
	h := newFixture(t)
	need := h.openNeed()
	svc := h.activeService(asBiz(h.provider), businesses.Coordinates{Latitude: 36.624, Longitude: 29.116})
	h.sessions.userID = h.provider
	rec := do(t, h, http.MethodGet, "/v1/needs/"+need.ID.String()+"/offer-context?businessId="+svc.BusinessID.String()+"&serviceId="+svc.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("context status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertNoUserIDs(t, rec.Body.String(), h.provider.String(), h.requester.String())
	if !strings.Contains(rec.Body.String(), `"title"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	rec = do(t, h, http.MethodPost, "/v1/needs/"+need.ID.String()+"/offers", allowedOrigin, createBody(svc), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/offers/mine", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("mine status = %d", rec.Code)
	}
	assertNoUserIDs(t, rec.Body.String(), h.provider.String(), h.requester.String())

	h.sessions.userID = mustOfferID(t)
	rec = do(t, h, http.MethodGet, "/v1/needs/"+need.ID.String()+"/offer-context?businessId="+svc.BusinessID.String()+"&serviceId="+svc.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unrelated context status = %d", rec.Code)
	}
}

type fixture struct {
	*Handler
	sessions  *fakeSessions
	needSvc   *needs.Service
	biz       *businesses.Service
	offerSvc  *offers.Service
	clock     *frozenNow
	requester offers.ID
	provider  offers.ID
}

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time { return f.now }

type fakeSessions struct {
	userID offers.ID
	err    error
}

func (f *fakeSessions) Resolve(ctx context.Context, rawToken string) (offers.ID, error) {
	if f.err != nil {
		return offers.ID{}, f.err
	}
	if rawToken != "session-token" {
		return offers.ID{}, ErrUnauthenticated
	}
	return f.userID, nil
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	clock := &frozenNow{now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	bizStore := businesses.NewMemoryStore()
	bizSvc, err := businesses.NewService(bizStore, nil, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	needStore := needs.NewMemoryStore()
	needSvc, err := needs.NewService(needStore, nil, bizSvc, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	offerStore := offers.NewMemoryStore()
	offerSvc, err := offers.NewService(offerStore, needSvc, bizSvc, bizSvc, bizSvc, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{userID: mustOfferID(t)}
	h, err := New(sessions, offerSvc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		Handler:   h,
		sessions:  sessions,
		needSvc:   needSvc,
		biz:       bizSvc,
		offerSvc:  offerSvc,
		clock:     clock,
		requester: mustOfferID(t),
		provider:  mustOfferID(t),
	}
}

func (h *fixture) openNeed() needs.Need {
	created, err := h.needSvc.Create(context.Background(), needs.ID(h.requester), needs.Content{
		Title:    "Need",
		Location: needs.Coordinates{Latitude: 36.621, Longitude: 29.116},
	})
	if err != nil {
		panic(err)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	opened, err := h.needSvc.Open(context.Background(), needs.ID(h.requester), created.ID)
	if err != nil {
		panic(err)
	}
	return opened
}

func (h *fixture) activeService(owner businesses.ID, loc businesses.Coordinates) businesses.OfferedService {
	profile, err := h.biz.Create(context.Background(), owner, businesses.ProfileContent{DisplayName: "Kafe"})
	if err != nil {
		panic(err)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	if _, err := h.biz.UpdateLocation(context.Background(), owner, profile.ID, &loc); err != nil {
		panic(err)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	if _, err := h.biz.Activate(context.Background(), owner, profile.ID); err != nil {
		panic(err)
	}
	offered, err := h.biz.CreateOfferedService(context.Background(), owner, profile.ID, businesses.ServiceContent{Title: "Tur"})
	if err != nil {
		panic(err)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	active, err := h.biz.ActivateOfferedService(context.Background(), owner, profile.ID, offered.ID)
	if err != nil {
		panic(err)
	}
	return active
}

func createBody(svc businesses.OfferedService) map[string]any {
	return map[string]any{
		"businessId": svc.BusinessID.String(),
		"serviceId":  svc.ID.String(),
		"message":    "Merhaba",
		"price":      map[string]any{"amount": "250", "currency": "TRY"},
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

func assertNoUserIDs(t *testing.T, body string, ids ...string) {
	t.Helper()
	lower := strings.ToLower(body)
	if strings.Contains(lower, "provideruserid") || strings.Contains(lower, "requesteruserid") || strings.Contains(lower, "owneruserid") {
		t.Fatalf("user id field leaked in %q", body)
	}
	for _, id := range ids {
		if id != "" && strings.Contains(body, id) {
			t.Fatalf("user uuid %s leaked in %q", id, body)
		}
	}
}

func mustOfferID(t *testing.T) offers.ID {
	t.Helper()
	id, err := offers.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustBizUser(t *testing.T) businesses.ID {
	t.Helper()
	id, err := businesses.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func asBiz(id offers.ID) businesses.ID {
	return businesses.ID(id)
}
