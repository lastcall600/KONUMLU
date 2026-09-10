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

	"backend/internal/deliveries"
	txncontracts "backend/internal/transactions/contracts"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newFixture(t)
	path := "/v1/transactions/" + h.txnID.String() + "/delivery"

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

func TestEligibleTransactionCreatesDeliveryIdempotent(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.requester
	path := "/v1/transactions/" + h.txnID.String() + "/delivery"
	rec := do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{
		"eligibility": "delivery_required",
		"method":      "handoff",
		"note":        "leave with concierge",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto deliveryDTO
	decode(t, rec, &dto)
	if dto.Status != string(deliveries.StatusPending) || dto.Eligibility != deliveries.EligibilityRequired {
		t.Fatalf("dto = %+v", dto)
	}
	if dto.Method == nil || *dto.Method != "handoff" {
		t.Fatalf("method = %+v", dto.Method)
	}
	assertPublicSafe(t, rec.Body.String(), h.requester.String(), h.provider.String())

	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{"method": "courier"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat status = %d", rec.Code)
	}
	var again deliveryDTO
	decode(t, rec, &again)
	if again.DeliveryID != dto.DeliveryID {
		t.Fatalf("repeat id %s vs %s", again.DeliveryID, dto.DeliveryID)
	}

	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{"requesterUserId": h.requester.String()}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("spoof status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{"address": "home", "gps": "1,1"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("address status = %d", rec.Code)
	}
}

func TestNonEligibleAndUnrelatedDenied(t *testing.T) {
	h := newFixture(t)
	h.txns.status = "cancelled"
	h.sessions.userID = h.requester
	rec := do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/delivery", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"not_eligible"`) {
		t.Fatalf("cancelled status = %d body=%s", rec.Code, rec.Body.String())
	}

	h.txns.status = "pending"
	h.sessions.userID = mustDelID(t)
	rec = do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/delivery", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger create status = %d", rec.Code)
	}
}

func TestParticipantReadLifecycleAndInvalidMethod(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.provider
	rec := do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/delivery", allowedOrigin, map[string]any{"method": "courier"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var created deliveryDTO
	decode(t, rec, &created)

	h.sessions.userID = mustDelID(t)
	rec = do(t, h, http.MethodGet, "/v1/deliveries/"+created.DeliveryID, "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger get status = %d", rec.Code)
	}

	h.sessions.userID = h.requester
	rec = do(t, h, http.MethodGet, "/v1/deliveries/"+created.DeliveryID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("requester get status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertPublicSafe(t, rec.Body.String(), h.requester.String(), h.provider.String())

	rec = do(t, h, http.MethodPost, "/v1/deliveries/"+created.DeliveryID+"/ready", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("requester ready status = %d", rec.Code)
	}

	h.sessions.userID = h.provider
	rec = do(t, h, http.MethodGet, "/v1/deliveries", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	assertPublicSafe(t, rec.Body.String(), h.requester.String(), h.provider.String())

	rec = do(t, h, http.MethodPost, "/v1/deliveries/"+created.DeliveryID+"/ready", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("ready status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/deliveries/"+created.DeliveryID+"/in-transit", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("in-transit status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/deliveries/"+created.DeliveryID+"/delivered", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("delivered status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/deliveries/"+created.DeliveryID+"/cancel", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("terminal cancel status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/delivery", allowedOrigin, map[string]any{"method": "drone"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat after delivered should be idempotent status = %d", rec.Code)
	}
}

func TestInvalidMethodOnCreate(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.requester
	rec := do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/delivery", allowedOrigin, map[string]any{"method": "drone"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid method status = %d body=%s", rec.Code, rec.Body.String())
	}
}

type fixture struct {
	*Handler
	sessions  *fakeSessions
	txns      *stubTxns
	requester deliveries.ID
	provider  deliveries.ID
	txnID     deliveries.ID
}

type fakeSessions struct {
	userID deliveries.ID
	err    error
}

func (f *fakeSessions) Resolve(ctx context.Context, rawToken string) (deliveries.ID, error) {
	if f.err != nil {
		return deliveries.ID{}, f.err
	}
	if rawToken != "session-token" {
		return deliveries.ID{}, ErrUnauthenticated
	}
	return f.userID, nil
}

type stubTxns struct {
	txnID     deliveries.ID
	requester deliveries.ID
	provider  deliveries.ID
	status    string
}

func (s *stubTxns) GetTransaction(ctx context.Context, transactionID txncontracts.ID) (txncontracts.TransactionRef, error) {
	if txncontracts.ID(s.txnID) != transactionID {
		return txncontracts.TransactionRef{}, txncontracts.ErrNotFound
	}
	return txncontracts.TransactionRef{
		ID:              transactionID,
		RequesterUserID: txncontracts.ID(s.requester),
		ProviderUserID:  txncontracts.ID(s.provider),
		Status:          s.status,
	}, nil
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	clock := &frozenNow{now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	requester := mustDelID(t)
	provider := mustDelID(t)
	txnID := mustDelID(t)
	txns := &stubTxns{
		txnID:     txnID,
		requester: requester,
		provider:  provider,
		status:    "pending",
	}
	svc, err := deliveries.NewService(deliveries.NewMemoryStore(), txns, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{userID: mustDelID(t)}
	h, err := New(sessions, svc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		Handler:   h,
		sessions:  sessions,
		txns:      txns,
		requester: requester,
		provider:  provider,
		txnID:     txnID,
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
	leaks := []string{"requesteruserid", "provideruserid", "requester_user_id", "provider_user_id"}
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

func mustDelID(t *testing.T) deliveries.ID {
	t.Helper()
	id, err := deliveries.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
