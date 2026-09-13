package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"backend/internal/identity"
	listingcontracts "backend/internal/listings/contracts"
	locationcontracts "backend/internal/location/contracts"
	"backend/internal/media"
	"backend/internal/notifications"
	"backend/internal/notifications/contracts"
	"backend/internal/platform/config"
	"backend/internal/platform/db"
	"backend/internal/platform/observability"
	"backend/internal/platform/outbox"
	"backend/internal/reviewaggregates"
	reviewscontracts "backend/internal/reviews/contracts"
	"backend/internal/search"
	"backend/internal/transactions"
	txncontracts "backend/internal/transactions/contracts"
	"backend/internal/trust"
	verifiedcontracts "backend/internal/verified/contracts"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	observability.ConfigureJSON(cfg, nil)

	if _, _, err := bindNotificationSenders(cfg); err != nil {
		return fmt.Errorf("notifications: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.OpenPool(ctx, cfg.DatabaseURL, cfg.DBPool)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ready(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}

	relay, err := newRelay(ctx, cfg, pool)
	if err != nil {
		return fmt.Errorf("outbox: %w", err)
	}

	// Email/SMS vendors are not selected. Missing senders fail retryably;
	// the worker must never complete an intent as a successful no-op send.
	slog.Info("outbox worker started")
	if blockers := cfg.AuthProviderLaunchBlockers(); len(blockers) > 0 {
		slog.Info("auth_provider_launch_blocked", "blockers", blockers)
	}
	if err := relay.RunWorkers(ctx, cfg.OutboxWorkerConcurrency); err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	slog.Info("outbox worker stopped")
	return nil
}

func newRelay(ctx context.Context, cfg config.Config, pool *db.Pool) (*outbox.Relay, error) {
	store := outbox.NewPostgresStore(pool)
	ob, err := outbox.New(store, outboxPolicy(cfg), nil)
	if err != nil {
		return nil, err
	}
	notifyStore := notifications.NewPostgresStore(pool)
	resolver, err := newIdentityDeliveryResolver(cfg, pool)
	if err != nil {
		return nil, err
	}
	wiring, err := productionNotificationsWiring(cfg, resolver)
	if err != nil {
		return nil, err
	}
	delivery, err := notifications.NewDeliveryService(notifyStore, wiring.Resolver, wiring.Email, wiring.SMS, nil)
	if err != nil {
		return nil, err
	}
	processHandler, mediaSvc, err := newMediaProcessHandler(cfg, pool)
	if err != nil {
		return nil, err
	}
	searchHandler, err := newSearchProjectionHandler(pool)
	if err != nil {
		return nil, err
	}
	trustHandler, err := newTrustProjectionHandler(pool)
	if err != nil {
		return nil, err
	}
	reviewAggHandler, err := newReviewAggregatesHandler(pool)
	if err != nil {
		return nil, err
	}
	completionHandler, err := newTransactionCompletionHandler(pool)
	if err != nil {
		return nil, err
	}
	reg, err := newHandlerRegistry(notifyStore, delivery, processHandler, searchHandler, trustHandler, reviewAggHandler, completionHandler)
	if err != nil {
		return nil, err
	}
	relay, err := outbox.NewRelay(ob, reg, cfg.OutboxPollInterval, nil)
	if err != nil {
		return nil, err
	}
	relay.SetLogf(func(format string, args ...any) {
		slog.Info("outbox", "detail", fmt.Sprintf(format, args...))
	})
	if mediaSvc != nil {
		go media.RunOrphanSweeper(ctx, mediaSvc, 0)
	}
	return relay, nil
}

func outboxPolicy(cfg config.Config) outbox.Policy {
	return outbox.Policy{
		BatchSize:         cfg.OutboxBatchSize,
		Lease:             cfg.OutboxLease,
		BackoffBase:       cfg.OutboxRetryBase,
		BackoffMultiplier: cfg.OutboxRetryMultiplier,
		BackoffCap:        cfg.OutboxRetryCap,
		Jitter:            cfg.OutboxRetryJitter,
	}
}

func newHandlerRegistry(store notifications.DeliveryPersister, delivery *notifications.DeliveryService, process, searchHandler, trustHandler, reviewAggHandler, completionHandler outbox.Handler) (*outbox.Registry, error) {
	h, err := notifications.NewIntentHandler(store, delivery)
	if err != nil {
		return nil, err
	}
	wh, err := notifications.NewWarningHandler(store)
	if err != nil {
		return nil, err
	}
	if process == nil {
		return nil, media.ErrStoreRequired
	}
	if searchHandler == nil {
		return nil, search.ErrStoreRequired
	}
	if trustHandler == nil {
		return nil, trust.ErrStoreRequired
	}
	if reviewAggHandler == nil {
		return nil, reviewaggregates.ErrStoreRequired
	}
	if completionHandler == nil {
		return nil, transactions.ErrStoreRequired
	}
	reg := outbox.NewRegistry()
	if err := reg.Register(contracts.IntentEventType, contracts.IntentEventVersion, h); err != nil {
		return nil, err
	}
	if err := reg.Register(contracts.WarningEventType, contracts.WarningEventVersion, wh); err != nil {
		return nil, err
	}
	if err := reg.Register(media.ProcessEventType, media.ProcessEventVersion, process); err != nil {
		return nil, err
	}
	if err := registerSearchHandlers(reg, searchHandler); err != nil {
		return nil, err
	}
	if err := registerTrustHandlers(reg, trustHandler); err != nil {
		return nil, err
	}
	if err := registerReviewAggregateHandlers(reg, reviewAggHandler, trustHandler); err != nil {
		return nil, err
	}
	if err := reg.Register(txncontracts.EventTypeCompleted, txncontracts.EventVersion, completionHandler); err != nil {
		return nil, err
	}
	if err := reg.Register(identity.AuthSecurityEventType, identity.AuthSecurityEventVersion, identity.NewAuthSecurityHandler()); err != nil {
		return nil, err
	}
	return reg, nil
}

func registerReviewAggregateHandlers(reg *outbox.Registry, reviewAgg, trustHandler outbox.Handler) error {
	return reg.Register(reviewscontracts.EventTypeVerifiedCreated, reviewscontracts.EventVersion, sequentialOutboxHandlers{reviewAgg, trustHandler})
}

func registerTrustHandlers(reg *outbox.Registry, h outbox.Handler) error {
	return reg.Register(verifiedcontracts.EventTypeInteractionCompleted, verifiedcontracts.EventVersion, h)
}

func registerSearchHandlers(reg *outbox.Registry, h outbox.Handler) error {
	events := []struct {
		typ     string
		version int
	}{
		{listingcontracts.EventTypePublished, listingcontracts.EventVersion},
		{listingcontracts.EventTypeUpdated, listingcontracts.EventVersion},
		{listingcontracts.EventTypeArchived, listingcontracts.EventVersion},
		{locationcontracts.EventTypeListingChanged, locationcontracts.EventVersion},
	}
	for _, e := range events {
		if err := reg.Register(e.typ, e.version, h); err != nil {
			return err
		}
	}
	return nil
}

// sequentialOutboxHandlers fans one outbox event to multiple domain handlers.
// The registry allows a single handler per event type/version.
type sequentialOutboxHandlers []outbox.Handler

func (s sequentialOutboxHandlers) Handle(ctx context.Context, event outbox.Event) error {
	for _, h := range s {
		if h == nil {
			return outbox.ErrHandlerRequired
		}
		if err := h.Handle(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
