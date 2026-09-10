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

	"backend/internal/payments"
	txncontracts "backend/internal/transactions/contracts"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newFixture(t)
	path := "/v1/transactions/" + h.txnID.String() + "/payment"

	rec := do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{}, map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}

	h.sessions.userID = h.payer
	rec = do(t, h, http.MethodPost, path, "", map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{}, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestPricedTransactionCreatesPaymentIdempotent(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.payer
	path := "/v1/transactions/" + h.txnID.String() + "/payment"
	rec := do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto paymentDTO
	decode(t, rec, &dto)
	if dto.Status != string(payments.StatusPending) || dto.Amount != "250.50" || dto.Currency != "TRY" {
		t.Fatalf("dto = %+v", dto)
	}
	assertPublicSafe(t, rec.Body.String(), h.payer.String(), h.payee.String())

	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat status = %d", rec.Code)
	}
	var again paymentDTO
	decode(t, rec, &again)
	if again.PaymentID != dto.PaymentID {
		t.Fatalf("repeat id %s vs %s", again.PaymentID, dto.PaymentID)
	}

	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{"amount": "1", "payerUserId": h.payer.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("spoof status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{"pan": "4111111111111111", "cvv": "123"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("card status = %d", rec.Code)
	}
}

func TestUnpricedAndUnrelatedDenied(t *testing.T) {
	h := newFixture(t)
	h.txns.price = nil
	h.sessions.userID = h.payer
	rec := do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/payment", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"not_priced"`) {
		t.Fatalf("unpriced status = %d body=%s", rec.Code, rec.Body.String())
	}

	h.txns.price = &txncontracts.Price{Amount: "10", Currency: "TRY"}
	h.sessions.userID = mustPayID(t)
	rec = do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/payment", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger create status = %d", rec.Code)
	}
}

func TestParticipantReadAndNoCaptureRoute(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.payer
	rec := do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/payment", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var created paymentDTO
	decode(t, rec, &created)

	h.sessions.userID = mustPayID(t)
	rec = do(t, h, http.MethodGet, "/v1/payments/"+created.PaymentID, "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger get status = %d", rec.Code)
	}

	h.sessions.userID = h.payee
	rec = do(t, h, http.MethodGet, "/v1/payments/"+created.PaymentID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("payee get status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertPublicSafe(t, rec.Body.String(), h.payer.String(), h.payee.String())

	rec = do(t, h, http.MethodGet, "/v1/payments", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	assertPublicSafe(t, rec.Body.String(), h.payer.String(), h.payee.String())

	rec = do(t, h, http.MethodPost, "/v1/payments/"+created.PaymentID+"/capture", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("fake capture status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/payments/"+created.PaymentID+"/authorize", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("fake authorize status = %d", rec.Code)
	}
}

type fixture struct {
	*Handler
	sessions *fakeSessions
	txns     *stubTxns
	payer    payments.ID
	payee    payments.ID
	txnID    payments.ID
}

type fakeSessions struct {
	userID payments.ID
	err    error
}

func (f *fakeSessions) Resolve(ctx context.Context, rawToken string) (payments.ID, error) {
	if f.err != nil {
		return payments.ID{}, f.err
	}
	if rawToken != "session-token" {
		return payments.ID{}, ErrUnauthenticated
	}
	return f.userID, nil
}

type stubTxns struct {
	txnID  payments.ID
	payer  payments.ID
	payee  payments.ID
	status string
	price  *txncontracts.Price
}

func (s *stubTxns) GetTransaction(ctx context.Context, transactionID txncontracts.ID) (txncontracts.TransactionRef, error) {
	if txncontracts.ID(s.txnID) != transactionID {
		return txncontracts.TransactionRef{}, txncontracts.ErrNotFound
	}
	return txncontracts.TransactionRef{
		ID:              transactionID,
		RequesterUserID: txncontracts.ID(s.payer),
		ProviderUserID:  txncontracts.ID(s.payee),
		Price:           s.price,
		Status:          s.status,
	}, nil
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	clock := &frozenNow{now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	payer := mustPayID(t)
	payee := mustPayID(t)
	txnID := mustPayID(t)
	txns := &stubTxns{
		txnID:  txnID,
		payer:  payer,
		payee:  payee,
		status: "pending",
		price:  &txncontracts.Price{Amount: "250.50", Currency: "TRY"},
	}
	svc, err := payments.NewService(payments.NewMemoryStore(), txns, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{userID: mustPayID(t)}
	h, err := New(sessions, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		Handler:  h,
		sessions: sessions,
		txns:     txns,
		payer:    payer,
		payee:    payee,
		txnID:    txnID,
	}
}

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time { return f.now }

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

func assertPublicSafe(t *testing.T, body string, ids ...string) {
	t.Helper()
	lower := strings.ToLower(body)
	leaks := []string{"payeruserid", "payeeuserid", "providerreference", "provider_reference", `"provider"`, "pan", "cvv"}
	for _, key := range leaks {
		if strings.Contains(lower, key) {
			t.Fatalf("leaked %s in %q", key, body)
		}
	}
	for _, id := range ids {
		if id != "" && strings.Contains(body, id) {
			t.Fatalf("user uuid %s leaked in %q", id, body)
		}
	}
}

func mustPayID(t *testing.T) payments.ID {
	t.Helper()
	id, err := payments.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
