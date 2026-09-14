package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/businesses"
	businesseshttp "backend/internal/businesses/httpapi"
	"backend/internal/deliveries"
	deliverieshttp "backend/internal/deliveries/httpapi"
	"backend/internal/disputes"
	disputeshttp "backend/internal/disputes/httpapi"
	"backend/internal/eids"
	"backend/internal/identity"
	identitycontracts "backend/internal/identity/contracts"
	eidsadapter "backend/internal/infrastructure/eids"
	staffidp "backend/internal/infrastructure/staffidp"
	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/moderation"
	moderationhttp "backend/internal/moderation/httpapi"
	"backend/internal/needs"
	needshttp "backend/internal/needs/httpapi"
	"backend/internal/offers"
	offershttp "backend/internal/offers/httpapi"
	"backend/internal/payments"
	paymentshttp "backend/internal/payments/httpapi"
	"backend/internal/platform/config"
	platcrypto "backend/internal/platform/crypto"
	"backend/internal/platform/health"
	"backend/internal/platform/observability"
	"backend/internal/staffauth"
	staffauthcontracts "backend/internal/staffauth/contracts"
	"backend/internal/transactions"
	transactionshttp "backend/internal/transactions/httpapi"
)

func TestHealthzRouteIndependentOfDatabase(t *testing.T) {
	mux := newMux(func(ctx context.Context) error {
		return errors.New("database unavailable")
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, staffRoutes{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body health.Response
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status field = %q, want ok", body.Status)
	}
}

func TestReadyzRouteOK(t *testing.T) {
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, staffRoutes{})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHealthzRouteIndependentOfValkey(t *testing.T) {
	mux := newMux(func(ctx context.Context) error {
		return errors.New("cache unavailable")
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, staffRoutes{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestComposeReadyRequiresValkeyWhenConfigured(t *testing.T) {
	ready := composeReady(
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return errors.New("cache unavailable") },
	)
	mux := newMux(ready, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, staffRoutes{})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var body health.Response
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "unavailable" {
		t.Fatalf("status field = %q, want unavailable", body.Status)
	}
}

func TestComposeReadySkipsNilValkeyCheck(t *testing.T) {
	ready := composeReady(func(ctx context.Context) error { return nil }, nil)
	if err := ready(context.Background()); err != nil {
		t.Fatalf("postgres-only ready: %v", err)
	}
}

func TestReadyzRouteUnavailable(t *testing.T) {
	mux := newMux(func(ctx context.Context) error {
		return errors.New("connection refused")
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, staffRoutes{})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var body health.Response
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "unavailable" {
		t.Fatalf("status field = %q, want unavailable", body.Status)
	}
}

func TestRunRequiresMaterialKeyConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv("IDENTITY_SESSION_IDLE", "1h")
	t.Setenv("IDENTITY_SESSION_ABSOLUTE", "24h")
	t.Setenv("IDENTITY_STEP_UP_TTL", "5m")
	t.Setenv("IDENTITY_WEBAUTHN_CEREMONY_TTL", "2m")
	t.Setenv("IDENTITY_AUTH_IP_MAX_ATTEMPTS", "20")
	t.Setenv("IDENTITY_AUTH_IP_WINDOW", "15m")
	t.Setenv("IDENTITY_AUTH_PASSWORD_USER_MAX_ATTEMPTS", "10")
	t.Setenv("IDENTITY_AUTH_PASSWORD_USER_WINDOW", "15m")
	t.Setenv("IDENTITY_VERIFICATION_CHALLENGE_TTL", "10m")
	t.Setenv("IDENTITY_VERIFICATION_CHALLENGE_MAX_ATTEMPTS", "5")
	t.Setenv("IDENTITY_VERIFICATION_PHONE_OTP_DIGITS", "6")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_DEST_MAX", "5")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_DEST_WINDOW", "1h")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_IP_MAX", "10")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_IP_WINDOW", "1h")
	t.Setenv("IDENTITY_SIGNUP_PROOF_TTL", "15m")
	t.Setenv("OUTBOX_BATCH_SIZE", "10")
	t.Setenv("OUTBOX_LEASE", "30s")
	t.Setenv("OUTBOX_POLL_INTERVAL", "1s")
	t.Setenv("OUTBOX_RETRY_BASE", "1m")
	t.Setenv("OUTBOX_RETRY_MULTIPLIER", "2")
	t.Setenv("OUTBOX_RETRY_CAP", "10m")
	t.Setenv("OUTBOX_RETRY_JITTER", "0s")
	t.Setenv("IDENTITY_VERIFICATION_MATERIAL_ACTIVE_KEY_ID", "")
	t.Setenv("IDENTITY_VERIFICATION_MATERIAL_KEYS", "")
	if err := run(); err == nil {
		t.Fatal("expected config error")
	}
}

func TestRunFailsWhenObjectStorageEnabledWithInvalidConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv("IDENTITY_SESSION_IDLE", "1h")
	t.Setenv("IDENTITY_SESSION_ABSOLUTE", "24h")
	t.Setenv("IDENTITY_STEP_UP_TTL", "5m")
	t.Setenv("IDENTITY_WEBAUTHN_CEREMONY_TTL", "2m")
	t.Setenv("IDENTITY_AUTH_IP_MAX_ATTEMPTS", "20")
	t.Setenv("IDENTITY_AUTH_IP_WINDOW", "15m")
	t.Setenv("IDENTITY_AUTH_PASSWORD_USER_MAX_ATTEMPTS", "10")
	t.Setenv("IDENTITY_AUTH_PASSWORD_USER_WINDOW", "15m")
	t.Setenv("IDENTITY_VERIFICATION_CHALLENGE_TTL", "10m")
	t.Setenv("IDENTITY_VERIFICATION_CHALLENGE_MAX_ATTEMPTS", "5")
	t.Setenv("IDENTITY_VERIFICATION_PHONE_OTP_DIGITS", "6")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_DEST_MAX", "5")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_DEST_WINDOW", "1h")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_IP_MAX", "10")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_IP_WINDOW", "1h")
	t.Setenv("IDENTITY_SIGNUP_PROOF_TTL", "15m")
	t.Setenv("OUTBOX_BATCH_SIZE", "10")
	t.Setenv("OUTBOX_LEASE", "30s")
	t.Setenv("OUTBOX_POLL_INTERVAL", "1s")
	t.Setenv("OUTBOX_RETRY_BASE", "1m")
	t.Setenv("OUTBOX_RETRY_MULTIPLIER", "2")
	t.Setenv("OUTBOX_RETRY_CAP", "10m")
	t.Setenv("OUTBOX_RETRY_JITTER", "0s")
	t.Setenv("IDENTITY_VERIFICATION_MATERIAL_ACTIVE_KEY_ID", "test-v1")
	t.Setenv("IDENTITY_VERIFICATION_MATERIAL_KEYS", "test-v1:ERERERERERERERERERERERERERERERERERERERERERE=")
	secret := "super-secret-object-key-xyz"
	t.Setenv("OBJECT_STORAGE_ENABLED", "true")
	t.Setenv("OBJECT_STORAGE_SECRET_KEY", secret)
	t.Setenv("OBJECT_STORAGE_ACCESS_KEY", "access-key")
	t.Setenv("OBJECT_STORAGE_REGION", "")
	t.Setenv("OBJECT_STORAGE_BUCKET", "")
	t.Setenv("OBJECT_STORAGE_UPLOAD_TTL", "")
	err := run()
	if err == nil {
		t.Fatal("expected object storage config error")
	}
	if !strings.Contains(err.Error(), "object storage") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("secret leaked: %v", err)
	}
}

func TestServerConstructsMaterialProtectorFromConfig(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 32)
	p, err := identity.NewMaterialProtectorFromDecodedKeys("v1", map[string][]byte{"v1": key})
	if err != nil || p == nil {
		t.Fatalf("protector err = %v", err)
	}
}

func TestServerConstructsPushEndpointAEADFromConfig(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 32)
	kr, err := platcrypto.NewSingleKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if kr.ActiveID() != "v1" {
		t.Fatalf("push encryption key id = %q, want v1", kr.ActiveID())
	}
	aead, err := platcrypto.NewAEAD(kr)
	if err != nil || aead == nil {
		t.Fatalf("aead err = %v", err)
	}
	hmacKey, err := platcrypto.NewHMACKey(bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hmacKey.Sum([]byte("x")); err != nil {
		t.Fatal(err)
	}
}

func TestBusinessesPublicRouteWired(t *testing.T) {
	store := businesses.NewMemoryStore()
	svc, err := businesses.NewService(store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := businesseshttp.New(wiredBusinessSessions{}, svc, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := businesses.NewID()
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, h, nil, nil, nil, nil, nil, nil, nil, staffRoutes{})
	req := httptest.NewRequest(http.MethodGet, "/v1/public/businesses/"+id.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"not_found"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	svcID, err := businesses.NewID()
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/public/services/"+svcID.String(), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("public service status = %d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/public/businesses/"+id.String()+"/services", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("public business services status = %d body=%s", rec.Code, rec.Body.String())
	}
}

type wiredBusinessSessions struct{}

func (wiredBusinessSessions) Resolve(ctx context.Context, rawToken string) (businesses.ID, error) {
	return businesses.ID{}, businesseshttp.ErrUnauthenticated
}

func TestNeedsOwnerRouteWired(t *testing.T) {
	store := needs.NewMemoryStore()
	svc, err := needs.NewService(store, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := needshttp.New(wiredNeedsSessions{}, svc, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, h, nil, nil, nil, nil, nil, nil, staffRoutes{})
	req := httptest.NewRequest(http.MethodGet, "/v1/needs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"unauthenticated"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	needID, err := needs.NewID()
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/needs/"+needID.String()+"/candidates", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("candidates status = %d body=%s", rec.Code, rec.Body.String())
	}
}

type wiredNeedsSessions struct{}

func (wiredNeedsSessions) Resolve(ctx context.Context, rawToken string) (needs.ID, error) {
	return needs.ID{}, needshttp.ErrUnauthenticated
}

func TestOffersRouteWired(t *testing.T) {
	needStore := needs.NewMemoryStore()
	needSvc, err := needs.NewService(needStore, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	offerStore := offers.NewMemoryStore()
	svc, err := offers.NewService(offerStore, needSvc, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := offershttp.New(wiredOffersSessions{}, svc, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, h, nil, nil, nil, nil, nil, staffRoutes{})
	req := httptest.NewRequest(http.MethodGet, "/v1/offers/mine", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"unauthenticated"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

type wiredOffersSessions struct{}

func (wiredOffersSessions) Resolve(ctx context.Context, rawToken string) (offers.ID, error) {
	return offers.ID{}, offershttp.ErrUnauthenticated
}

func TestTransactionsRouteWired(t *testing.T) {
	needStore := needs.NewMemoryStore()
	needSvc, err := needs.NewService(needStore, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	offerStore := offers.NewMemoryStore()
	offerSvc, err := offers.NewService(offerStore, needSvc, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	txnSvc, err := transactions.NewService(transactions.NewMemoryStore(), offerSvc, needSvc, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := transactionshttp.New(wiredTransactionsSessions{}, txnSvc, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := transactions.NewID()
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, h, nil, nil, nil, nil, staffRoutes{})
	req := httptest.NewRequest(http.MethodGet, "/v1/transactions/"+id.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"unauthenticated"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

type wiredTransactionsSessions struct{}

func (wiredTransactionsSessions) Resolve(ctx context.Context, rawToken string) (transactions.ID, error) {
	return transactions.ID{}, transactionshttp.ErrUnauthenticated
}

func TestPaymentsRouteWired(t *testing.T) {
	txnSvc, err := transactions.NewService(transactions.NewMemoryStore(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	paySvc, err := payments.NewService(payments.NewMemoryStore(), txnSvc, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := paymentshttp.New(wiredPaymentsSessions{}, paySvc, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := payments.NewID()
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, h, nil, nil, nil, staffRoutes{})
	req := httptest.NewRequest(http.MethodGet, "/v1/payments/"+id.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"unauthenticated"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/payments/"+id.String()+"/capture", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("fake capture status = %d body=%s", rec.Code, rec.Body.String())
	}
}

type wiredPaymentsSessions struct{}

func (wiredPaymentsSessions) Resolve(ctx context.Context, rawToken string) (payments.ID, error) {
	return payments.ID{}, paymentshttp.ErrUnauthenticated
}

func TestDeliveriesRouteWired(t *testing.T) {
	txnSvc, err := transactions.NewService(transactions.NewMemoryStore(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	delSvc, err := deliveries.NewService(deliveries.NewMemoryStore(), txnSvc, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := deliverieshttp.New(wiredDeliveriesSessions{}, delSvc, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := deliveries.NewID()
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, h, nil, nil, staffRoutes{})
	req := httptest.NewRequest(http.MethodGet, "/v1/deliveries/"+id.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"unauthenticated"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

type wiredDeliveriesSessions struct{}

func (wiredDeliveriesSessions) Resolve(ctx context.Context, rawToken string) (deliveries.ID, error) {
	return deliveries.ID{}, deliverieshttp.ErrUnauthenticated
}

func TestDisputesRouteWired(t *testing.T) {
	txnSvc, err := transactions.NewService(transactions.NewMemoryStore(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	delSvc, err := deliveries.NewService(deliveries.NewMemoryStore(), txnSvc, nil)
	if err != nil {
		t.Fatal(err)
	}
	dispSvc, err := disputes.NewService(disputes.NewMemoryStore(), txnSvc, delSvc, disputes.DefaultPolicy(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := disputeshttp.New(wiredDisputesSessions{}, dispSvc, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := disputes.NewID()
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, h, nil, staffRoutes{})
	req := httptest.NewRequest(http.MethodGet, "/v1/disputes/"+id.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"unauthenticated"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/staff/disputes/"+id.String()+"/review", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("production staff dispute status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStaffRoutesUnregisteredWithoutProvider(t *testing.T) {
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, staffRoutes{})
	for _, path := range []string{
		"/v1/staff/moderation/reports",
		"/v1/staff/identity/profiles/00000000-0000-4000-8000-000000000001",
		"/v1/staff/listings/00000000-0000-4000-8000-000000000001",
		"/v1/staff/trust/profiles/00000000-0000-4000-8000-000000000001",
		"/v1/staff/review-aggregates/listings/00000000-0000-4000-8000-000000000001",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s unconfigured status = %d", path, rec.Code)
		}
	}
}

func TestProductionEIDSGatewayNeverVerifies(t *testing.T) {
	g := eidsadapter.Unconfigured{}
	prop, err := g.VerifyProperty(context.Background(), eids.PropertyVerifyRequest{})
	if err != nil || prop.Outcome != eids.OutcomeUnavailable {
		t.Fatalf("property = %+v err=%v", prop, err)
	}
	veh, err := g.VerifyVehicle(context.Background(), eids.VehicleVerifyRequest{})
	if err != nil || veh.Outcome != eids.OutcomeUnavailable {
		t.Fatalf("vehicle = %+v err=%v", veh, err)
	}
}

func TestStaffAuthorizerUnconfiguredAndConfiguredWithoutAdapter(t *testing.T) {
	authz, err := newStaffAuthorizer(config.Config{})
	if err != nil || authz != nil {
		t.Fatalf("unconfigured authz=%v err=%v", authz, err)
	}
	_, err = newStaffAuthorizer(config.Config{StaffIDP: config.StaffIDP{
		Issuer:   "https://idp.example.test",
		Audience: "konumlu-staff",
		JWKSURL:  "https://idp.example.test/jwks",
	}})
	if err == nil {
		t.Fatal("expected fail-closed adapter required")
	}
}

func TestStaffAuthorizerProductionRejectsDevAdapter(t *testing.T) {
	staffID, err := staffauthcontracts.NewID()
	if err != nil {
		t.Fatal(err)
	}
	dev := config.StaffDevIDP{
		Enabled: true,
		Token:   "local-dev-staff-token",
		StaffID: staffID.String(),
		Roles:   []string{"moderator"},
	}
	_, err = newStaffAuthorizer(config.Config{Environment: config.EnvProduction, StaffDevIDP: dev})
	if !errors.Is(err, staffidp.ErrDevForbidden) {
		t.Fatalf("production err=%v", err)
	}
	_, err = newStaffAuthorizer(config.Config{Environment: config.EnvStaging, StaffDevIDP: dev})
	if !errors.Is(err, staffidp.ErrDevForbidden) {
		t.Fatalf("staging err=%v", err)
	}
}

func TestStaffDevAuthorizerQueueRoute(t *testing.T) {
	staffID, err := staffauthcontracts.NewID()
	if err != nil {
		t.Fatal(err)
	}
	authz, err := newStaffAuthorizer(config.Config{
		Environment: config.EnvDevelopment,
		StaffDevIDP: config.StaffDevIDP{
			Enabled: true,
			Token:   "local-dev-staff-token",
			StaffID: staffID.String(),
			Roles:   []string{"moderator"},
		},
	})
	if err != nil || authz == nil {
		t.Fatalf("authz=%v err=%v", authz, err)
	}
	svc, err := moderation.NewService(moderation.NewMemoryStore(), staffTestListings{}, staffTestProfiles{}, moderation.DefaultPolicy(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := moderationhttp.NewStaff(authz, svc)
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, staffRoutes{moderation: sh})

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-konumlu_session", Value: "consumer"})
	req.Header.Set("X-Staff-Role", "admin")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("consumer session status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports", nil)
	req.Header.Set("Authorization", "Bearer local-dev-staff-token")
	req.Header.Set("X-Staff-Role", "admin")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"reports"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

type staffTestListings struct{}

func (staffTestListings) ResolveListingOwner(context.Context, listingcontracts.ID) (listingcontracts.ListingRef, error) {
	return listingcontracts.ListingRef{}, listingcontracts.ErrNotFound
}

func (staffTestListings) AssertListingOwnedBy(context.Context, listingcontracts.ID, listingcontracts.ID) error {
	return listingcontracts.ErrNotFound
}

type staffTestProfiles struct{}

func (staffTestProfiles) ResolveByPublicID(context.Context, identitycontracts.ID) (identitycontracts.PublicProfile, error) {
	return identitycontracts.PublicProfile{}, identitycontracts.ErrNotFound
}

func (staffTestProfiles) ResolveByUserID(context.Context, identitycontracts.ID) (identitycontracts.PublicProfile, error) {
	return identitycontracts.PublicProfile{}, identitycontracts.ErrNotFound
}

func (staffTestProfiles) ResolveUserIDByPublicID(context.Context, identitycontracts.ID) (identitycontracts.ID, error) {
	return identitycontracts.ID{}, identitycontracts.ErrNotFound
}

func TestStaffDisputeRoutesRegisteredWhenAuthorizerPresent(t *testing.T) {
	txnSvc, err := transactions.NewService(transactions.NewMemoryStore(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	delSvc, err := deliveries.NewService(deliveries.NewMemoryStore(), txnSvc, nil)
	if err != nil {
		t.Fatal(err)
	}
	dispSvc, err := disputes.NewService(disputes.NewMemoryStore(), txnSvc, delSvc, disputes.DefaultPolicy(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := disputeshttp.New(wiredDisputesSessions{}, dispSvc, []string{"https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	staffID, err := staffauthcontracts.NewID()
	if err != nil {
		t.Fatal(err)
	}
	authz, err := staffauth.NewAuthorizer(staticCmdStaffProvider{principal: staffauthcontracts.Principal{
		StaffID: staffID,
		Roles:   []staffauthcontracts.Role{staffauthcontracts.RoleSupport},
	}}, staffauth.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	sh, err := disputeshttp.NewStaff(authz, dispSvc)
	if err != nil {
		t.Fatal(err)
	}
	id, err := disputes.NewID()
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(func(ctx context.Context) error { return nil }, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, h, nil, staffRoutes{disputes: sh})

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/disputes/"+id.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated staff = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/staff/disputes/"+id.String(), nil)
	req.AddCookie(&http.Cookie{Name: "__Host-konumlu_session", Value: "consumer"})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("consumer session staff = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/disputes/"+id.String(), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("consumer dispute route = %d", rec.Code)
	}
}

type staticCmdStaffProvider struct {
	principal staffauthcontracts.Principal
}

func (s staticCmdStaffProvider) Verify(_ context.Context, cred staffauthcontracts.Credential) (staffauthcontracts.Principal, error) {
	if cred.Token != "staff-token" {
		return staffauthcontracts.Principal{}, staffauthcontracts.ErrUnauthenticated
	}
	return s.principal, nil
}

type wiredDisputesSessions struct{}

func (wiredDisputesSessions) Resolve(ctx context.Context, rawToken string) (disputes.ID, error) {
	return disputes.ID{}, disputeshttp.ErrUnauthenticated
}

func TestWrappedHealthzStillIndependentOfReady(t *testing.T) {
	h := observability.Wrap(newMux(func(ctx context.Context) error {
		return errors.New("database unavailable")
	}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, staffRoutes{}))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing request id")
	}
}

func TestShutdownHTTP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	go srv.Serve(ln)
	if err := shutdownHTTP(srv, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestProductionConfigFailClosedFromServerEnv(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://konumlu:super-secret-db@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv("IDENTITY_SESSION_IDLE", "1h")
	t.Setenv("IDENTITY_SESSION_ABSOLUTE", "24h")
	t.Setenv("IDENTITY_STEP_UP_TTL", "5m")
	t.Setenv("IDENTITY_WEBAUTHN_CEREMONY_TTL", "2m")
	t.Setenv("IDENTITY_AUTH_IP_MAX_ATTEMPTS", "20")
	t.Setenv("IDENTITY_AUTH_IP_WINDOW", "15m")
	t.Setenv("IDENTITY_AUTH_PASSWORD_USER_MAX_ATTEMPTS", "10")
	t.Setenv("IDENTITY_AUTH_PASSWORD_USER_WINDOW", "15m")
	t.Setenv("IDENTITY_VERIFICATION_CHALLENGE_TTL", "10m")
	t.Setenv("IDENTITY_VERIFICATION_CHALLENGE_MAX_ATTEMPTS", "5")
	t.Setenv("IDENTITY_VERIFICATION_PHONE_OTP_DIGITS", "6")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_DEST_MAX", "5")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_DEST_WINDOW", "1h")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_IP_MAX", "10")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_IP_WINDOW", "1h")
	t.Setenv("IDENTITY_SIGNUP_PROOF_TTL", "15m")
	t.Setenv("OUTBOX_BATCH_SIZE", "10")
	t.Setenv("OUTBOX_LEASE", "30s")
	t.Setenv("OUTBOX_POLL_INTERVAL", "1s")
	t.Setenv("OUTBOX_RETRY_BASE", "1m")
	t.Setenv("OUTBOX_RETRY_MULTIPLIER", "2")
	t.Setenv("OUTBOX_RETRY_CAP", "10m")
	t.Setenv("OUTBOX_RETRY_JITTER", "0s")
	t.Setenv("IDENTITY_VERIFICATION_MATERIAL_ACTIVE_KEY_ID", "test-v1")
	t.Setenv("IDENTITY_VERIFICATION_MATERIAL_KEYS", "test-v1:ERERERERERERERERERERERERERERERERERERERERERE=")
	err := run()
	if err == nil {
		t.Fatal("expected production config failure")
	}
	if strings.Contains(err.Error(), "super-secret-db") {
		t.Fatalf("leaked: %v", err)
	}
}
