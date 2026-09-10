package main

import (
	"context"
	"errors"

	"backend/internal/identity"
	notifyinfra "backend/internal/infrastructure/notifications"
	objstorage "backend/internal/infrastructure/storage"
	"backend/internal/listings"
	"backend/internal/location"
	"backend/internal/media"
	"backend/internal/needs"
	"backend/internal/notifications"
	"backend/internal/platform/config"
	"backend/internal/platform/db"
	"backend/internal/reviewaggregates"
	"backend/internal/search"
	"backend/internal/transactions"
	"backend/internal/trust"
)

// notificationsWiring is explicit composition-root delivery configuration.
// Disabled channels leave senders nil. Missing external adapters fail wiring.
// An intent for an absent channel fails retryably; this process must never
// pretend a message was sent.
type notificationsWiring struct {
	Resolver notifications.VerificationMaterialResolver
	Email    notifications.EmailSender
	SMS      notifications.SMSSender
}

func productionNotificationTransports() notifyinfra.Transports {
	// Vendor clients are not selected. External mode must not start without one.
	return notifyinfra.Transports{}
}

func bindNotificationSenders(cfg config.Config) (notifications.EmailSender, notifications.SMSSender, error) {
	senders, err := notifyinfra.Bind(cfg.NotificationsEmailMode, cfg.NotificationsSMSMode, productionNotificationTransports())
	if err != nil {
		return nil, nil, err
	}
	return senders.Email, senders.SMS, nil
}

func productionNotificationsWiring(cfg config.Config, resolver notifications.VerificationMaterialResolver) (notificationsWiring, error) {
	email, sms, err := bindNotificationSenders(cfg)
	if err != nil {
		return notificationsWiring{}, err
	}
	return notificationsWiring{
		Resolver: resolver,
		Email:    email,
		SMS:      sms,
	}, nil
}

func newIdentityDeliveryResolver(cfg config.Config, pool *db.Pool) (notifications.VerificationMaterialResolver, error) {
	protector, err := identity.NewMaterialProtectorFromDecodedKeys(cfg.MaterialKeys.ActiveID, cfg.MaterialKeys.Keys)
	if err != nil {
		return nil, err
	}
	challenges, err := identity.NewChallenges(identity.NewPostgresStore(pool), identity.VerificationChallengePolicy{}, nil, protector, nil)
	if err != nil {
		return nil, err
	}
	return newIdentityMaterialResolver(challenges)
}

// identityDeliverySource is the Identity operation the composition root adapts.
type identityDeliverySource interface {
	ResolveVerificationDelivery(ctx context.Context, id identity.ID) (identity.VerificationDelivery, error)
}

// identityMaterialResolver adapts Identity resolution to the Notifications port.
// cmd/worker may import both domains. Notifications must not import Identity.
type identityMaterialResolver struct {
	src identityDeliverySource
}

func newIdentityMaterialResolver(src identityDeliverySource) (*identityMaterialResolver, error) {
	if src == nil {
		return nil, notifications.ErrStoreRequired
	}
	return &identityMaterialResolver{src: src}, nil
}

func (r *identityMaterialResolver) Resolve(ctx context.Context, challengeID notifications.ID) (notifications.VerificationMaterial, error) {
	if r == nil || r.src == nil {
		return notifications.VerificationMaterial{}, notifications.ErrStoreRequired
	}
	if challengeID.IsZero() {
		return notifications.VerificationMaterial{}, notifications.ErrUnavailable
	}
	var id identity.ID
	copy(id[:], challengeID[:])
	got, err := r.src.ResolveVerificationDelivery(ctx, id)
	if err != nil {
		return notifications.VerificationMaterial{}, mapIdentityDeliveryErr(err)
	}
	kind, err := mapVerificationKind(got.Kind)
	if err != nil {
		return notifications.VerificationMaterial{}, err
	}
	if got.DestinationCanonical == "" || got.Secret == "" {
		return notifications.VerificationMaterial{}, notifications.ErrMaterialUnusable
	}
	return notifications.VerificationMaterial{
		Kind:        kind,
		Destination: got.DestinationCanonical,
		Secret:      got.Secret,
	}, nil
}

func mapVerificationKind(kind identity.IdentifierKind) (notifications.MaterialKind, error) {
	switch kind {
	case identity.IdentifierEmail:
		return notifications.MaterialKindEmail, nil
	case identity.IdentifierPhone:
		return notifications.MaterialKindPhone, nil
	default:
		return "", notifications.ErrMaterialUnusable
	}
}

func mapIdentityDeliveryErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, identity.ErrChallengeExpired) ||
		errors.Is(err, identity.ErrChallengeConsumed) ||
		errors.Is(err, identity.ErrChallengeExhausted) ||
		errors.Is(err, identity.ErrInvalidChallenge) ||
		errors.Is(err, identity.ErrZeroID) {
		return notifications.ErrMaterialUnusable
	}
	if errors.Is(err, identity.ErrUnavailable) {
		return notifications.ErrUnavailable
	}
	return notifications.ErrUnavailable
}

var _ notifications.VerificationMaterialResolver = (*identityMaterialResolver)(nil)

func newMediaProcessHandler(cfg config.Config, pool *db.Pool) (*media.ProcessHandler, error) {
	objects, maxBytes, err := openMediaObjects()
	if err != nil {
		return nil, err
	}
	svc, err := media.NewService(media.NewPostgresStore(pool), objects, nil)
	if err != nil {
		return nil, err
	}
	svc.SetMaxUploadBytes(maxBytes)
	svc.SetProcessingPolicy(media.ProcessingPolicy{
		RequireMalwareScan: cfg.MediaMalwareScanRequired,
		RequireModeration:  cfg.MediaImageModerationRequired,
		MaxBytes:           maxBytes,
	})
	return media.NewProcessHandler(svc)
}

func openMediaObjects() (media.ObjectStorage, int64, error) {
	cfg, err := objstorage.Load()
	if err != nil {
		return nil, 0, err
	}
	if !cfg.Enabled {
		return disabledObjectStorage{}, 0, nil
	}
	adapter, err := objstorage.New(cfg)
	if err != nil {
		return nil, 0, err
	}
	return adapter, adapter.MaxUploadBytes(), nil
}

func newSearchProjectionHandler(pool *db.Pool) (*search.ProjectionHandler, error) {
	listingSvc, err := listings.NewService(listings.NewPostgresStore(pool), nil, nil)
	if err != nil {
		return nil, err
	}
	locSvc, err := location.NewService(location.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	projector, err := search.NewProjector(
		search.NewPostgresStore(pool),
		listingSvc,
		location.NewListingLocations(locSvc),
	)
	if err != nil {
		return nil, err
	}
	return search.NewProjectionHandler(projector)
}

func newTrustProjectionHandler(pool *db.Pool) (*trust.ProjectionHandler, error) {
	projector, err := trust.NewProjector(trust.NewPostgresStore(pool), trust.DefaultLevelPolicy())
	if err != nil {
		return nil, err
	}
	return trust.NewProjectionHandler(projector)
}

func newReviewAggregatesHandler(pool *db.Pool) (*reviewaggregates.ProjectionHandler, error) {
	projector, err := reviewaggregates.NewProjector(reviewaggregates.NewPostgresStore(pool))
	if err != nil {
		return nil, err
	}
	return reviewaggregates.NewProjectionHandler(projector)
}

func newTransactionCompletionHandler(pool *db.Pool) (*transactions.CompletionHandler, error) {
	needSvc, err := needs.NewService(needs.NewPostgresStore(pool), nil, nil, nil)
	if err != nil {
		return nil, err
	}
	return transactions.NewCompletionHandler(transactions.NewPostgresStore(pool), needSvc)
}

// disabledObjectStorage fails retryably when object storage is not configured.
// It never silently succeeds processing.
type disabledObjectStorage struct{}

func (disabledObjectStorage) IssueUploadTarget(context.Context, string) (media.UploadTarget, error) {
	return media.UploadTarget{}, media.ErrUnavailable
}
func (disabledObjectStorage) IssueGetTarget(context.Context, string) (media.GetTarget, error) {
	return media.GetTarget{}, media.ErrUnavailable
}
func (disabledObjectStorage) Stat(context.Context, string) (media.ObjectStat, error) {
	return media.ObjectStat{}, media.ErrUnavailable
}
func (disabledObjectStorage) GetObject(context.Context, string) ([]byte, media.ObjectStat, error) {
	return nil, media.ObjectStat{}, media.ErrUnavailable
}
func (disabledObjectStorage) PutObject(context.Context, string, []byte, string) error {
	return media.ErrUnavailable
}
func (disabledObjectStorage) DeleteObject(context.Context, string) error {
	return media.ErrUnavailable
}

var _ media.ObjectStorage = disabledObjectStorage{}
