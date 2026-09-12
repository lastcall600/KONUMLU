package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	notifyinfra "backend/internal/infrastructure/notifications"
	listingcontracts "backend/internal/listings/contracts"
	locationcontracts "backend/internal/location/contracts"
	"backend/internal/media"
	"backend/internal/needs"
	"backend/internal/notifications"
	"backend/internal/notifications/contracts"
	"backend/internal/platform/config"
	"backend/internal/platform/outbox"
	"backend/internal/reviewaggregates"
	reviewscontracts "backend/internal/reviews/contracts"
	"backend/internal/search"
	"backend/internal/transactions"
	txncontracts "backend/internal/transactions/contracts"
	"backend/internal/trust"
	verifiedcontracts "backend/internal/verified/contracts"
)

func TestRunRequiresConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	err := run()
	if err == nil {
		t.Fatal("expected config error")
	}
	if strings.Contains(err.Error(), "postgres://") || strings.Contains(strings.ToLower(err.Error()), "password") {
		t.Fatalf("error leaked secrets: %v", err)
	}
}

func TestNewHandlerRegistryRegistersIntentHandler(t *testing.T) {
	store := notifications.NewMemoryStore()
	delivery := mustTestDelivery(t, store)
	reg, err := newHandlerRegistry(store, delivery, mustTestMediaHandler(t), mustTestSearchHandler(t), mustTestTrustHandler(t), mustTestReviewAggregatesHandler(t), mustTestCompletionHandler(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Lookup(contracts.IntentEventType, contracts.IntentEventVersion); !ok {
		t.Fatal("cmd/worker must register notifications.intent v1")
	}
	if _, ok := reg.Lookup(contracts.WarningEventType, contracts.WarningEventVersion); !ok {
		t.Fatal("cmd/worker must register notifications.moderation.warning v1")
	}
	if _, ok := reg.Lookup(media.ProcessEventType, media.ProcessEventVersion); !ok {
		t.Fatal("cmd/worker must register media.image.process v1")
	}
	if _, ok := reg.Lookup(listingcontracts.EventTypePublished, listingcontracts.EventVersion); !ok {
		t.Fatal("cmd/worker must register listings.listing.published v1")
	}
	if _, ok := reg.Lookup(listingcontracts.EventTypeUpdated, listingcontracts.EventVersion); !ok {
		t.Fatal("cmd/worker must register listings.listing.updated v1")
	}
	if _, ok := reg.Lookup(listingcontracts.EventTypeArchived, listingcontracts.EventVersion); !ok {
		t.Fatal("cmd/worker must register listings.listing.archived v1")
	}
	if _, ok := reg.Lookup(locationcontracts.EventTypeListingChanged, locationcontracts.EventVersion); !ok {
		t.Fatal("cmd/worker must register location.listing.changed v1")
	}
	if _, ok := reg.Lookup(verifiedcontracts.EventTypeInteractionCompleted, verifiedcontracts.EventVersion); !ok {
		t.Fatal("cmd/worker must register verified.interaction.completed v1")
	}
	if _, ok := reg.Lookup(reviewscontracts.EventTypeVerifiedCreated, reviewscontracts.EventVersion); !ok {
		t.Fatal("cmd/worker must register reviews.verified.created v1")
	}
	if _, ok := reg.Lookup(txncontracts.EventTypeCompleted, txncontracts.EventVersion); !ok {
		t.Fatal("cmd/worker must register transactions.transaction.completed v1")
	}
	if _, ok := reg.Lookup(contracts.IntentEventType, 2); ok {
		t.Fatal("unknown version must not be registered")
	}
	if _, ok := reg.Lookup(media.ProcessEventType, 2); ok {
		t.Fatal("unknown media process version must not be registered")
	}
}

func TestOutboxPolicyFromConfig(t *testing.T) {
	cfg := config.Config{
		OutboxBatchSize:       5,
		OutboxLease:           15 * time.Second,
		OutboxPollInterval:    2 * time.Second,
		OutboxRetryBase:       time.Minute,
		OutboxRetryMultiplier: 2,
		OutboxRetryCap:        10 * time.Minute,
		OutboxRetryJitter:     time.Second,
	}
	p := outboxPolicy(cfg)
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.BatchSize != 5 || p.Lease != 15*time.Second || p.BackoffBase != time.Minute || p.Jitter != time.Second {
		t.Fatalf("policy mismatch: %+v", p)
	}
	if p.BackoffMultiplier != 2 || p.BackoffCap != 10*time.Minute {
		t.Fatalf("retry mismatch: %+v", p)
	}
}

func TestWorkerConcurrencyFromConfigLoad(t *testing.T) {
	setWorkerRequiredEnv(t)
	t.Setenv("OUTBOX_WORKER_CONCURRENCY", "3")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OutboxWorkerConcurrency != 3 {
		t.Fatalf("workers = %d", cfg.OutboxWorkerConcurrency)
	}
}

func TestEmptyRegistryUnknownDoesNotComplete(t *testing.T) {
	if _, err := outbox.NewRelay(nil, outbox.NewRegistry(), time.Second, nil); !errors.Is(err, outbox.ErrStoreRequired) {
		t.Fatalf("nil store: %v", err)
	}
}

func mustTestCompletionHandler(t *testing.T) *transactions.CompletionHandler {
	t.Helper()
	needSvc, err := needs.NewService(needs.NewMemoryStore(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := transactions.NewCompletionHandler(transactions.NewMemoryStore(), needSvc)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustTestReviewAggregatesHandler(t *testing.T) *reviewaggregates.ProjectionHandler {
	t.Helper()
	p, err := reviewaggregates.NewProjector(reviewaggregates.NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	h, err := reviewaggregates.NewProjectionHandler(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustTestTrustHandler(t *testing.T) *trust.ProjectionHandler {
	t.Helper()
	p, err := trust.NewProjector(trust.NewMemoryStore(), trust.DefaultLevelPolicy())
	if err != nil {
		t.Fatal(err)
	}
	h, err := trust.NewProjectionHandler(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustTestSearchHandler(t *testing.T) *search.ProjectionHandler {
	t.Helper()
	p, err := search.NewProjector(search.NewMemoryStore(), stubSearchListings{}, stubSearchGeo{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := search.NewProjectionHandler(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

type stubSearchListings struct{}

func (stubSearchListings) GetListingSnapshot(context.Context, listingcontracts.ID) (listingcontracts.ListingSnapshot, error) {
	return listingcontracts.ListingSnapshot{}, listingcontracts.ErrNotFound
}

type stubSearchGeo struct{}

func (stubSearchGeo) GetListingLocation(context.Context, locationcontracts.ID) (locationcontracts.ListingPoint, error) {
	return locationcontracts.ListingPoint{}, locationcontracts.ErrNotFound
}

func mustTestMediaHandler(t *testing.T) *media.ProcessHandler {
	t.Helper()
	svc, err := media.NewService(media.NewMemoryStore(), media.NewMemoryObjectStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := media.NewProcessHandler(svc)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustTestDelivery(t *testing.T, store notifications.DeliveryPersister) *notifications.DeliveryService {
	t.Helper()
	resolver, err := newIdentityMaterialResolver(&stubIdentitySource{})
	if err != nil {
		t.Fatal(err)
	}
	wiring := mustDisabledWiring(t, resolver)
	svc, err := notifications.NewDeliveryService(store, wiring.Resolver, wiring.Email, wiring.SMS, nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
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
	err := run()
	if err == nil {
		t.Fatal("expected config error")
	}
	if strings.Contains(strings.ToLower(err.Error()), "password") {
		t.Fatalf("error leaked secrets: %v", err)
	}
}

func TestRunFailsWhenExternalEmailAdapterMissing(t *testing.T) {
	setWorkerRequiredEnv(t)
	t.Setenv("NOTIFICATIONS_EMAIL_MODE", "external")
	t.Setenv("NOTIFICATIONS_SMS_MODE", "disabled")
	err := run()
	if err == nil {
		t.Fatal("expected startup error")
	}
	if !errors.Is(err, notifyinfra.ErrEmailAdapterRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRejectsNoopNotificationMode(t *testing.T) {
	setWorkerRequiredEnv(t)
	t.Setenv("NOTIFICATIONS_EMAIL_MODE", "noop")
	err := run()
	if err == nil {
		t.Fatal("expected config error")
	}
}

func setWorkerRequiredEnv(t *testing.T) {
	t.Helper()
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
	enc := "ERERERERERERERERERERERERERERERERERERERERERE="
	t.Setenv("IDENTITY_VERIFICATION_MATERIAL_ACTIVE_KEY_ID", "test-v1")
	t.Setenv("IDENTITY_VERIFICATION_MATERIAL_KEYS", "test-v1:"+enc)
}
