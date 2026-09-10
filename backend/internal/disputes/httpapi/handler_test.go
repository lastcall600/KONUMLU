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

	"backend/internal/disputes"
	txncontracts "backend/internal/transactions/contracts"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newFixture(t)
	path := "/v1/transactions/" + h.txnID.String() + "/dispute"

	rec := do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{"reasonCode": "other"}, map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}

	h.sessions.userID = h.requester
	rec = do(t, h, http.MethodPost, path, "", map[string]any{"reasonCode": "other"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{"reasonCode": "other"}, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestParticipantOpensDisputeAndDTOHidesUserUUIDs(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.requester
	path := "/v1/transactions/" + h.txnID.String() + "/dispute"
	rec := do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{
		"reasonCode": "item_or_service_not_as_described",
		"statement":  "not the item shown",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto disputeDTO
	decode(t, rec, &dto)
	if dto.Status != string(disputes.StatusOpen) || dto.OpenedByRole != disputes.RoleRequester {
		t.Fatalf("dto = %+v", dto)
	}
	assertPublicSafe(t, rec.Body.String(), h.requester.String(), h.provider.String())

	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{"reasonCode": "payment_issue"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat status = %d", rec.Code)
	}
	var again disputeDTO
	decode(t, rec, &again)
	if again.DisputeID != dto.DisputeID {
		t.Fatalf("repeat id %s vs %s", again.DisputeID, dto.DisputeID)
	}

	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{
		"reasonCode":      "other",
		"openedByUserId":  h.provider.String(),
		"requesterUserId": h.requester.String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("spoof status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStrangerAndIneligibleDenied(t *testing.T) {
	h := newFixture(t)
	h.txns.status = "cancelled"
	h.sessions.userID = h.requester
	rec := do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/dispute", allowedOrigin, map[string]any{"reasonCode": "other"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"not_eligible"`) {
		t.Fatalf("cancelled status = %d body=%s", rec.Code, rec.Body.String())
	}

	h.txns.status = "pending"
	h.sessions.userID = mustDispID(t)
	rec = do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/dispute", allowedOrigin, map[string]any{"reasonCode": "other"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger create status = %d", rec.Code)
	}
}

func TestParticipantReadEvidenceWithdrawAndPrivacy(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.requester
	rec := do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/dispute", allowedOrigin, map[string]any{"reasonCode": "other"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var created disputeDTO
	decode(t, rec, &created)

	h.sessions.userID = mustDispID(t)
	rec = do(t, h, http.MethodGet, "/v1/disputes/"+created.DisputeID, "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger get status = %d", rec.Code)
	}

	h.sessions.userID = h.provider
	rec = do(t, h, http.MethodGet, "/v1/disputes/"+created.DisputeID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("provider get status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertPublicSafe(t, rec.Body.String(), h.requester.String(), h.provider.String())

	rec = do(t, h, http.MethodGet, "/v1/disputes", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	assertPublicSafe(t, rec.Body.String(), h.requester.String(), h.provider.String())

	rec = do(t, h, http.MethodPost, "/v1/disputes/"+created.DisputeID+"/evidence", allowedOrigin, map[string]any{
		"evidenceType": "party_statement",
		"title":        "my note",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("evidence status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertPublicSafe(t, rec.Body.String(), h.requester.String(), h.provider.String())

	rec = do(t, h, http.MethodGet, "/v1/disputes/"+created.DisputeID+"/evidence", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("list evidence status = %d", rec.Code)
	}

	h.sessions.userID = h.requester
	rec = do(t, h, http.MethodPost, "/v1/disputes/"+created.DisputeID+"/withdraw", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestInvalidReasonAndConsumerStaffPathsAbsent(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.requester
	rec := do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/dispute", allowedOrigin, map[string]any{"reasonCode": "chargeback"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid reason status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/staff/disputes/"+mustDispID(t).String()+"/review", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("consumer staff review status = %d", rec.Code)
	}
}

type fixture struct {
	*Handler
	sessions  *fakeSessions
	txns      *stubTxns
	requester disputes.ID
	provider  disputes.ID
	txnID     disputes.ID
}

type fakeSessions struct {
	userID disputes.ID
	err    error
}

func (f *fakeSessions) Resolve(ctx context.Context, rawToken string) (disputes.ID, error) {
	if f.err != nil {
		return disputes.ID{}, f.err
	}
	if rawToken != "session-token" {
		return disputes.ID{}, ErrUnauthenticated
	}
	return f.userID, nil
}

type stubTxns struct {
	txnID     disputes.ID
	requester disputes.ID
	provider  disputes.ID
	status    string
	created   time.Time
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
		CreatedAt:       s.created,
	}, nil
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	created := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	clock := &frozenNow{now: created.Add(24 * time.Hour)}
	requester := mustDispID(t)
	provider := mustDispID(t)
	txnID := mustDispID(t)
	txns := &stubTxns{
		txnID:     txnID,
		requester: requester,
		provider:  provider,
		status:    "pending",
		created:   created,
	}
	svc, err := disputes.NewService(disputes.NewMemoryStore(), txns, nil, disputes.DefaultPolicy(), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{userID: mustDispID(t)}
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
	leaks := []string{"openedbyuserid", "requesteruserid", "provideruserid", "actoruserid", "opened_by_user_id"}
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

func mustDispID(t *testing.T) disputes.ID {
	t.Helper()
	id, err := disputes.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
