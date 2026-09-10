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
	"backend/internal/platform/outbox"
	"backend/internal/transactions"
	txncontracts "backend/internal/transactions/contracts"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newFixture(t)
	offer := h.acceptedOffer()
	path := "/v1/offers/" + offer.ID.String() + "/transaction"

	rec := do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{}, map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}

	h.sessions.userID = h.requester
	rec = do(t, h, http.MethodPost, path, "", map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{}, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestAcceptedOfferCreatesTransactionAndIdempotent(t *testing.T) {
	h := newFixture(t)
	offer := h.acceptedOffer()
	h.sessions.userID = h.requester
	path := "/v1/offers/" + offer.ID.String() + "/transaction"
	rec := do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto transactionDTO
	decode(t, rec, &dto)
	if dto.Status != string(transactions.StatusPending) || dto.AgreedPrice == nil || dto.AgreedPrice.Amount != "250" {
		t.Fatalf("dto = %+v", dto)
	}
	assertNoUserIDs(t, rec.Body.String(), h.provider.String(), h.requester.String())

	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat status = %d", rec.Code)
	}
	var again transactionDTO
	decode(t, rec, &again)
	if again.TransactionID != dto.TransactionID {
		t.Fatalf("repeat id %s vs %s", again.TransactionID, dto.TransactionID)
	}

	owned, err := h.needSvc.GetOwned(context.Background(), needs.ID(h.requester), needs.ID(offer.NeedID))
	if err != nil || owned.Status != needs.StatusOpen {
		t.Fatalf("need after create = %+v err=%v", owned, err)
	}

	body := map[string]any{"requesterUserId": mustTxnID(t).String()}
	rec = do(t, h, http.MethodPost, path, allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("spoof status = %d", rec.Code)
	}
}

func TestCancelledNeedRejectsCreateAndStart(t *testing.T) {
	h := newFixture(t)
	offer := h.acceptedOffer()
	h.sessions.userID = h.requester
	h.clock.now = h.clock.now.Add(time.Minute)
	if _, err := h.needSvc.Cancel(context.Background(), needs.ID(h.requester), needs.ID(offer.NeedID)); err != nil {
		t.Fatal(err)
	}
	path := "/v1/offers/" + offer.ID.String() + "/transaction"
	rec := do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("cancelled create status = %d body=%s", rec.Code, rec.Body.String())
	}

	h2 := newFixture(t)
	offer2 := h2.acceptedOffer()
	h2.sessions.userID = h2.requester
	rec = do(t, h2, http.MethodPost, "/v1/offers/"+offer2.ID.String()+"/transaction", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var created transactionDTO
	decode(t, rec, &created)
	h2.clock.now = h2.clock.now.Add(time.Minute)
	needID, err := needs.ParseID(created.NeedID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h2.needSvc.Cancel(context.Background(), needs.ID(h2.requester), needID); err != nil {
		t.Fatal(err)
	}
	rec = do(t, h2, http.MethodPost, "/v1/transactions/"+created.TransactionID+"/start", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("cancelled start status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestNonAcceptedAndUnrelatedDenied(t *testing.T) {
	h := newFixture(t)
	submitted := h.submittedOffer()
	h.sessions.userID = h.requester
	rec := do(t, h, http.MethodPost, "/v1/offers/"+submitted.ID.String()+"/transaction", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("submitted status = %d body=%s", rec.Code, rec.Body.String())
	}

	h.clock.now = h.clock.now.Add(time.Minute)
	offer, err := h.offerSvc.Accept(context.Background(), offers.ID(h.requester), submitted.NeedID, submitted.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.sessions.userID = mustTxnID(t)
	rec = do(t, h, http.MethodPost, "/v1/offers/"+offer.ID.String()+"/transaction", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger create status = %d", rec.Code)
	}
	h.sessions.userID = h.provider
	rec = do(t, h, http.MethodPost, "/v1/offers/"+offer.ID.String()+"/transaction", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("provider create status = %d", rec.Code)
	}
}

func TestLifecycleAndParticipantRead(t *testing.T) {
	h := newFixture(t)
	offer := h.acceptedOffer()
	h.sessions.userID = h.requester
	rec := do(t, h, http.MethodPost, "/v1/offers/"+offer.ID.String()+"/transaction", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var created transactionDTO
	decode(t, rec, &created)

	h.sessions.userID = mustTxnID(t)
	rec = do(t, h, http.MethodGet, "/v1/transactions/"+created.TransactionID, "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger get status = %d", rec.Code)
	}

	h.sessions.userID = h.provider
	rec = do(t, h, http.MethodGet, "/v1/transactions/"+created.TransactionID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("provider get status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertNoUserIDs(t, rec.Body.String(), h.provider.String(), h.requester.String())

	rec = do(t, h, http.MethodPost, "/v1/transactions/"+created.TransactionID+"/start", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("start status = %d body=%s", rec.Code, rec.Body.String())
	}
	h.sessions.userID = h.requester
	rec = do(t, h, http.MethodPost, "/v1/transactions/"+created.TransactionID+"/complete", allowedOrigin, map[string]any{"fulfilled": true}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fulfill spoof status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/transactions/"+created.TransactionID+"/complete", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := h.dispatchCompleted(t, created.TransactionID); err != nil {
		t.Fatal(err)
	}
	needID, err := needs.ParseID(created.NeedID)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := h.needSvc.GetOwned(context.Background(), needs.ID(h.requester), needID)
	if err != nil || owned.Status != needs.StatusFulfilled {
		t.Fatalf("need after complete = %+v err=%v", owned, err)
	}
	rec = do(t, h, http.MethodPost, "/v1/transactions/"+created.TransactionID+"/complete", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("complete replay status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := h.dispatchCompleted(t, created.TransactionID); err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodPost, "/v1/transactions/"+created.TransactionID+"/cancel", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("completed cancel status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodGet, "/v1/transactions", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	assertNoUserIDs(t, rec.Body.String(), h.provider.String(), h.requester.String())
	if strings.Contains(rec.Body.String(), `"userId"`) {
		t.Fatalf("userId leaked: %s", rec.Body.String())
	}
}

type fixture struct {
	*Handler
	sessions  *fakeSessions
	needSvc   *needs.Service
	txnStore  *transactions.MemoryStore
	biz       *businesses.Service
	offerSvc  *offers.Service
	clock     *frozenNow
	requester transactions.ID
	provider  transactions.ID
}

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time { return f.now }

type fakeSessions struct {
	userID transactions.ID
	err    error
}

func (f *fakeSessions) Resolve(ctx context.Context, rawToken string) (transactions.ID, error) {
	if f.err != nil {
		return transactions.ID{}, f.err
	}
	if rawToken != "session-token" {
		return transactions.ID{}, ErrUnauthenticated
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
	txnStore := transactions.NewMemoryStore()
	txnSvc, err := transactions.NewService(txnStore, offerSvc, needSvc, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	txnSvc.SetOutbox(nil, &httpMemoryEnqueuer{})
	sessions := &fakeSessions{userID: mustTxnID(t)}
	h, err := New(sessions, txnSvc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		Handler:   h,
		sessions:  sessions,
		needSvc:   needSvc,
		txnStore:  txnStore,
		biz:       bizSvc,
		offerSvc:  offerSvc,
		clock:     clock,
		requester: mustTxnID(t),
		provider:  mustTxnID(t),
	}
}

func (h *fixture) dispatchCompleted(t *testing.T, transactionID string) error {
	t.Helper()
	id, err := transactions.ParseID(transactionID)
	if err != nil {
		return err
	}
	txn, err := h.txnStore.Get(context.Background(), id)
	if err != nil {
		return err
	}
	handler, err := transactions.NewCompletionHandler(h.txnStore, h.needSvc)
	if err != nil {
		return err
	}
	ev, err := encodeHTTPCompleted(txn)
	if err != nil {
		return err
	}
	return handler.Handle(context.Background(), ev)
}

func (h *fixture) submittedOffer() offers.Offer {
	need := h.openNeed()
	svc := h.activeService(asBiz(h.provider), businesses.Coordinates{Latitude: 36.624, Longitude: 29.116})
	h.clock.now = h.clock.now.Add(time.Minute)
	offer, err := h.offerSvc.Create(context.Background(), offers.ID(h.provider), offers.Content{
		NeedID:             offers.ID(need.ID),
		ProviderBusinessID: offers.ID(svc.BusinessID),
		ServiceID:          offers.ID(svc.ID),
		Price:              &offers.Price{Amount: "250", Currency: "TRY"},
	})
	if err != nil {
		panic(err)
	}
	return offer
}

func (h *fixture) acceptedOffer() offers.Offer {
	offer := h.submittedOffer()
	h.clock.now = h.clock.now.Add(time.Minute)
	accepted, err := h.offerSvc.Accept(context.Background(), offers.ID(h.requester), offer.NeedID, offer.ID)
	if err != nil {
		panic(err)
	}
	return accepted
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

func mustTxnID(t *testing.T) transactions.ID {
	t.Helper()
	id, err := transactions.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func asBiz(id transactions.ID) businesses.ID {
	return businesses.ID(id)
}

type httpMemoryEnqueuer struct{}

func (e *httpMemoryEnqueuer) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	return outbox.Event{ID: id, EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
}

func encodeHTTPCompleted(txn transactions.Transaction) (outbox.Event, error) {
	if txn.CompletedAt == nil {
		return outbox.Event{}, transactions.ErrInvalidTxn
	}
	payload, err := json.Marshal(txncontracts.CompletedPayload{
		TransactionID:   txn.ID.String(),
		OfferID:         txn.OfferID.String(),
		NeedID:          txn.NeedID.String(),
		RequesterUserID: txn.RequesterUserID.String(),
		CompletedAt:     txn.CompletedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return outbox.Event{}, err
	}
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	return outbox.Event{
		ID:           id,
		EventType:    txncontracts.EventTypeCompleted,
		EventVersion: txncontracts.EventVersion,
		Payload:      payload,
	}, nil
}
