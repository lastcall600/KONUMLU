package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backend/internal/businesses"
	businesseshttp "backend/internal/businesses/httpapi"
	"backend/internal/deliveries"
	deliverieshttp "backend/internal/deliveries/httpapi"
	"backend/internal/disputes"
	disputeshttp "backend/internal/disputes/httpapi"
	"backend/internal/eids"
	"backend/internal/favorites"
	favoriteshttp "backend/internal/favorites/httpapi"
	"backend/internal/identity"
	"backend/internal/identity/httpapi"
	"backend/internal/identity/publicprofile"
	eidsadapter "backend/internal/infrastructure/eids"
	"backend/internal/infrastructure/staffidp"
	objstorage "backend/internal/infrastructure/storage"
	"backend/internal/listings"
	listingshttp "backend/internal/listings/httpapi"
	"backend/internal/location"
	"backend/internal/masterdata"
	masterdatahttp "backend/internal/masterdata/httpapi"
	"backend/internal/media"
	mediacontracts "backend/internal/media/contracts"
	mediahttp "backend/internal/media/httpapi"
	"backend/internal/messaging"
	messaginghttp "backend/internal/messaging/httpapi"
	"backend/internal/moderation"
	moderationhttp "backend/internal/moderation/httpapi"
	"backend/internal/needs"
	needshttp "backend/internal/needs/httpapi"
	"backend/internal/offers"
	offershttp "backend/internal/offers/httpapi"
	"backend/internal/payments"
	paymentshttp "backend/internal/payments/httpapi"
	"backend/internal/platform/cache"
	"backend/internal/platform/config"
	"backend/internal/platform/db"
	"backend/internal/platform/health"
	"backend/internal/platform/httpx"
	"backend/internal/platform/observability"
	"backend/internal/platform/outbox"
	"backend/internal/reviewaggregates"
	reviewaggregateshttp "backend/internal/reviewaggregates/httpapi"
	"backend/internal/reviews"
	reviewshttp "backend/internal/reviews/httpapi"
	"backend/internal/savedsearch"
	savedsearchhttp "backend/internal/savedsearch/httpapi"
	"backend/internal/search"
	searchhttp "backend/internal/search/httpapi"
	"backend/internal/staffauth"
	staffauthcontracts "backend/internal/staffauth/contracts"
	"backend/internal/transactions"
	transactionshttp "backend/internal/transactions/httpapi"
	"backend/internal/trust"
	trusthttp "backend/internal/trust/httpapi"
	"backend/internal/verified"
	verifiedhttp "backend/internal/verified/httpapi"
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

	objCfg, err := objstorage.Load()
	if err != nil {
		return fmt.Errorf("object storage: %w", err)
	}
	var objects media.ObjectStorage
	var maxUploadBytes int64
	if objCfg.Enabled {
		adapter, err := objstorage.New(objCfg)
		if err != nil {
			return fmt.Errorf("object storage: %w", err)
		}
		objects = adapter
		maxUploadBytes = adapter.MaxUploadBytes()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.OpenPool(ctx, cfg.DatabaseURL, cfg.DBPool)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ready(ctx); err != nil {
		log.Print("database not ready at startup")
	}

	cacheClient, cacheReady, err := openCache(ctx, cfg.ValkeyURL)
	if err != nil {
		return fmt.Errorf("cache: %w", err)
	}
	if cacheClient != nil {
		defer cacheClient.Close()
	}

	identityStore := identity.NewPostgresStore(pool)
	sessions, err := identity.NewSessions(identityStore, identity.SessionPolicy{
		Idle:     cfg.SessionIdle,
		Absolute: cfg.SessionAbsolute,
	}, cacheClient, identity.SessionCachePolicy{}, nil)
	if err != nil {
		return fmt.Errorf("identity sessions: %w", err)
	}

	identityHandler, err := newIdentityHTTP(pool, cfg, cacheClient, sessions)
	if err != nil {
		return fmt.Errorf("identity http: %w", err)
	}

	publicProfileHandler, err := newPublicProfileHTTP(pool, cfg, sessions)
	if err != nil {
		return fmt.Errorf("public profile http: %w", err)
	}

	mediaHandler, err := newMediaHTTP(pool, cfg, sessions, objects, maxUploadBytes)
	if err != nil {
		return fmt.Errorf("media http: %w", err)
	}

	listingsHandler, err := newListingsHTTP(pool, cfg, sessions, objects)
	if err != nil {
		return fmt.Errorf("listings http: %w", err)
	}

	masterdataHandler, err := newMasterDataHTTP(pool)
	if err != nil {
		return fmt.Errorf("master data http: %w", err)
	}

	searchHandler, err := newSearchHTTP(pool)
	if err != nil {
		return fmt.Errorf("search http: %w", err)
	}

	favoritesHandler, err := newFavoritesHTTP(pool, cfg, sessions)
	if err != nil {
		return fmt.Errorf("favorites http: %w", err)
	}

	savedSearchHandler, err := newSavedSearchHTTP(pool, cfg, sessions)
	if err != nil {
		return fmt.Errorf("saved search http: %w", err)
	}

	messagingHandler, err := newMessagingHTTP(pool, cfg, sessions)
	if err != nil {
		return fmt.Errorf("messaging http: %w", err)
	}

	verifiedHandler, err := newVerifiedHTTP(pool, cfg, sessions)
	if err != nil {
		return fmt.Errorf("verified http: %w", err)
	}

	trustHandler, err := newTrustHTTP(pool, cfg, sessions)
	if err != nil {
		return fmt.Errorf("trust http: %w", err)
	}

	reviewsHandler, err := newReviewsHTTP(pool, cfg, sessions)
	if err != nil {
		return fmt.Errorf("reviews http: %w", err)
	}

	reviewAggregatesHandler, err := newReviewAggregatesHTTP(pool, cfg, sessions)
	if err != nil {
		return fmt.Errorf("review aggregates http: %w", err)
	}

	staffAuth, err := newStaffAuthorizer(cfg)
	if err != nil {
		return fmt.Errorf("staff identity provider: %w", err)
	}

	moderationHandler, moderationStaff, err := newModerationHTTP(pool, cfg, sessions, staffAuth)
	if err != nil {
		return fmt.Errorf("moderation http: %w", err)
	}

	businessesHandler, needsHandler, offersHandler, transactionsHandler, paymentsHandler, deliveriesHandler, disputesHandler, disputesStaff, err := newBusinessesNeedsAndOffersHTTP(pool, cfg, sessions, staffAuth)
	if err != nil {
		return fmt.Errorf("marketplace http: %w", err)
	}

	identityStaff, err := newIdentityStaffHTTP(pool, staffAuth)
	if err != nil {
		return fmt.Errorf("identity staff http: %w", err)
	}
	listingsStaff, err := newListingsStaffHTTP(pool, staffAuth, objects)
	if err != nil {
		return fmt.Errorf("listings staff http: %w", err)
	}
	trustStaff, err := newTrustStaffHTTP(pool, staffAuth)
	if err != nil {
		return fmt.Errorf("trust staff http: %w", err)
	}
	reviewStaff, err := newReviewAggregatesStaffHTTP(pool, staffAuth)
	if err != nil {
		return fmt.Errorf("review aggregates staff http: %w", err)
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: observability.Wrap(newMux(composeReady(pool.Ready, cacheReady), identityHandler, publicProfileHandler, mediaHandler, listingsHandler, masterdataHandler, searchHandler, favoritesHandler, savedSearchHandler, messagingHandler, verifiedHandler, trustHandler, reviewsHandler, reviewAggregatesHandler, moderationHandler, businessesHandler, needsHandler, offersHandler, transactionsHandler, paymentsHandler, deliveriesHandler, disputesHandler, staffRoutes{
			moderation: moderationStaff,
			disputes:   disputesStaff,
			identity:   identityStaff,
			listings:   listingsStaff,
			trust:      trustStaff,
			reviews:    reviewStaff,
		})),
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	return shutdownHTTP(srv, cfg.ShutdownTimeout)
}

func shutdownHTTP(srv *http.Server, timeout time.Duration) error {
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return srv.Shutdown(ctx)
}

func newMux(ready health.CheckFunc, identityHandler *httpapi.Handler, publicProfileHandler *publicprofile.Handler, mediaHandler *mediahttp.Handler, listingsHandler *listingshttp.Handler, masterdataHandler *masterdatahttp.Handler, searchHandler *searchhttp.Handler, favoritesHandler *favoriteshttp.Handler, savedSearchHandler *savedsearchhttp.Handler, messagingHandler *messaginghttp.Handler, verifiedHandler *verifiedhttp.Handler, trustHandler *trusthttp.Handler, reviewsHandler *reviewshttp.Handler, reviewAggregatesHandler *reviewaggregateshttp.Handler, moderationHandler *moderationhttp.Handler, businessesHandler *businesseshttp.Handler, needsHandler *needshttp.Handler, offersHandler *offershttp.Handler, transactionsHandler *transactionshttp.Handler, paymentsHandler *paymentshttp.Handler, deliveriesHandler *deliverieshttp.Handler, disputesHandler *disputeshttp.Handler, staff staffRoutes) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.Handler())
	mux.Handle("GET /readyz", health.ReadyHandler(ready))
	if identityHandler != nil {
		identityHandler.Register(mux)
	}
	if publicProfileHandler != nil {
		publicProfileHandler.Register(mux)
	}
	if mediaHandler != nil {
		mediaHandler.Register(mux)
	}
	if listingsHandler != nil {
		listingsHandler.Register(mux)
	}
	if masterdataHandler != nil {
		masterdataHandler.Register(mux)
	}
	if searchHandler != nil {
		searchHandler.Register(mux)
	}
	if favoritesHandler != nil {
		favoritesHandler.Register(mux)
	}
	if savedSearchHandler != nil {
		savedSearchHandler.Register(mux)
	}
	if messagingHandler != nil {
		messagingHandler.Register(mux)
	}
	if verifiedHandler != nil {
		verifiedHandler.Register(mux)
	}
	if trustHandler != nil {
		trustHandler.Register(mux)
	}
	if reviewsHandler != nil {
		reviewsHandler.Register(mux)
	}
	if reviewAggregatesHandler != nil {
		reviewAggregatesHandler.Register(mux)
	}
	if moderationHandler != nil {
		moderationHandler.Register(mux)
	}
	if businessesHandler != nil {
		businessesHandler.Register(mux)
	}
	if needsHandler != nil {
		needsHandler.Register(mux)
	}
	if offersHandler != nil {
		offersHandler.Register(mux)
	}
	if transactionsHandler != nil {
		transactionsHandler.Register(mux)
	}
	if paymentsHandler != nil {
		paymentsHandler.Register(mux)
	}
	if deliveriesHandler != nil {
		deliveriesHandler.Register(mux)
	}
	if disputesHandler != nil {
		disputesHandler.Register(mux)
	}
	if staff.moderation != nil {
		staff.moderation.Register(mux)
	}
	if staff.disputes != nil {
		staff.disputes.Register(mux)
	}
	if staff.identity != nil {
		staff.identity.Register(mux)
	}
	if staff.listings != nil {
		staff.listings.Register(mux)
	}
	if staff.trust != nil {
		staff.trust.Register(mux)
	}
	if staff.reviews != nil {
		staff.reviews.Register(mux)
	}
	return mux
}

type staffRoutes struct {
	moderation *moderationhttp.StaffHandler
	disputes   *disputeshttp.StaffHandler
	identity   *publicprofile.StaffHandler
	listings   *listingshttp.StaffHandler
	trust      *trusthttp.StaffHandler
	reviews    *reviewaggregateshttp.StaffHandler
}

func newStaffAuthorizer(cfg config.Config) (staffauthcontracts.Authorizer, error) {
	provider, err := staffidp.Resolve(cfg)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, nil
	}
	return staffauth.NewAuthorizer(provider, staffauth.DefaultPolicy())
}

func newIdentityStaffHTTP(pool *db.Pool, staffAuth staffauthcontracts.Authorizer) (*publicprofile.StaffHandler, error) {
	if staffAuth == nil {
		return nil, nil
	}
	svc, err := publicprofile.NewService(publicprofile.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	return publicprofile.NewStaff(staffAuth, svc)
}

func newListingsStaffHTTP(pool *db.Pool, staffAuth staffauthcontracts.Authorizer, objects media.ObjectStorage) (*listingshttp.StaffHandler, error) {
	if staffAuth == nil {
		return nil, nil
	}
	mdSvc, err := masterdata.NewService(masterdata.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	listingSvc, err := listings.NewService(listings.NewPostgresStore(pool), masterdata.NewPublishedForms(mdSvc), nil)
	if err != nil {
		return nil, err
	}
	locSvc, err := location.NewService(location.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	var publicMedia mediacontracts.PublicListingMedia
	if objects != nil {
		mediaSvc, err := media.NewService(media.NewPostgresStore(pool), objects, nil)
		if err != nil {
			return nil, err
		}
		publicMedia = media.NewPublicListingMedia(mediaSvc)
	}
	profileSvc, err := publicprofile.NewService(publicprofile.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	profiles, err := publicprofile.NewResolver(profileSvc)
	if err != nil {
		return nil, err
	}
	return listingshttp.NewStaff(staffAuth, listingSvc, location.NewListingLocations(locSvc), publicMedia, profiles)
}

func newTrustStaffHTTP(pool *db.Pool, staffAuth staffauthcontracts.Authorizer) (*trusthttp.StaffHandler, error) {
	if staffAuth == nil {
		return nil, nil
	}
	projector, err := trust.NewProjector(trust.NewPostgresStore(pool), trust.DefaultLevelPolicy())
	if err != nil {
		return nil, err
	}
	svc, err := trust.NewService(projector)
	if err != nil {
		return nil, err
	}
	profileSvc, err := publicprofile.NewService(publicprofile.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	profiles, err := publicprofile.NewResolver(profileSvc)
	if err != nil {
		return nil, err
	}
	return trusthttp.NewStaff(staffAuth, svc, profiles)
}

func newReviewAggregatesStaffHTTP(pool *db.Pool, staffAuth staffauthcontracts.Authorizer) (*reviewaggregateshttp.StaffHandler, error) {
	if staffAuth == nil {
		return nil, nil
	}
	projector, err := reviewaggregates.NewProjector(reviewaggregates.NewPostgresStore(pool))
	if err != nil {
		return nil, err
	}
	svc, err := reviewaggregates.NewService(projector)
	if err != nil {
		return nil, err
	}
	return reviewaggregateshttp.NewStaff(staffAuth, svc)
}

func newBusinessesNeedsAndOffersHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions, staffAuth staffauthcontracts.Authorizer) (*businesseshttp.Handler, *needshttp.Handler, *offershttp.Handler, *transactionshttp.Handler, *paymentshttp.Handler, *deliverieshttp.Handler, *disputeshttp.Handler, *disputeshttp.StaffHandler, error) {
	mdSvc, err := masterdata.NewService(masterdata.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	cats := masterdata.NewPublishedCategories(mdSvc)
	bizSvc, err := businesses.NewService(businesses.NewPostgresStore(pool), cats, nil)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	bizHandler, err := businesseshttp.New(identityBusinessesSessions{sessions: sessions}, bizSvc, cfg.WebAuthnRPOrigins)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	needSvc, err := needs.NewService(needs.NewPostgresStore(pool), cats, bizSvc, nil)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	needHandler, err := needshttp.New(identityNeedsSessions{sessions: sessions}, needSvc, cfg.WebAuthnRPOrigins)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	offerSvc, err := offers.NewService(offers.NewPostgresStore(pool), needSvc, bizSvc, bizSvc, bizSvc, nil)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	offerHandler, err := offershttp.New(identityOffersSessions{sessions: sessions}, offerSvc, cfg.WebAuthnRPOrigins)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	txnSvc, err := transactions.NewService(transactions.NewPostgresStore(pool), offerSvc, needSvc, nil)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	ob, err := outbox.New(outbox.NewPostgresStore(pool), outbox.Policy{
		BatchSize:         cfg.OutboxBatchSize,
		Lease:             cfg.OutboxLease,
		BackoffBase:       cfg.OutboxRetryBase,
		BackoffMultiplier: cfg.OutboxRetryMultiplier,
		BackoffCap:        cfg.OutboxRetryCap,
		Jitter:            cfg.OutboxRetryJitter,
	}, nil)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	txnSvc.SetOutbox(transactions.PoolTransactor{Pool: pool}, ob)
	txnHandler, err := transactionshttp.New(identityTransactionsSessions{sessions: sessions}, txnSvc, cfg.WebAuthnRPOrigins)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	paySvc, err := payments.NewService(payments.NewPostgresStore(pool), txnSvc, nil)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	payHandler, err := paymentshttp.New(identityPaymentsSessions{sessions: sessions}, paySvc, cfg.WebAuthnRPOrigins)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	delSvc, err := deliveries.NewService(deliveries.NewPostgresStore(pool), txnSvc, nil)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	delHandler, err := deliverieshttp.New(identityDeliveriesSessions{sessions: sessions}, delSvc, cfg.WebAuthnRPOrigins)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	dispSvc, err := disputes.NewService(disputes.NewPostgresStore(pool), txnSvc, delSvc, disputes.DefaultPolicy(), nil)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	dispHandler, err := disputeshttp.New(identityDisputesSessions{sessions: sessions}, dispSvc, cfg.WebAuthnRPOrigins)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}
	var dispStaff *disputeshttp.StaffHandler
	if staffAuth != nil {
		dispStaff, err = disputeshttp.NewStaff(staffAuth, dispSvc)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, nil, nil, err
		}
	}
	return bizHandler, needHandler, offerHandler, txnHandler, payHandler, delHandler, dispHandler, dispStaff, nil
}

type identityNeedsSessions struct {
	sessions *identity.Sessions
}

func (s identityNeedsSessions) Resolve(ctx context.Context, rawToken string) (needs.ID, error) {
	if s.sessions == nil {
		return needs.ID{}, needs.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return needs.ID{}, needshttp.ErrUnauthenticated
		}
		return needs.ID{}, needs.ErrUnavailable
	}
	var id needs.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

type identityBusinessesSessions struct {
	sessions *identity.Sessions
}

func (s identityBusinessesSessions) Resolve(ctx context.Context, rawToken string) (businesses.ID, error) {
	if s.sessions == nil {
		return businesses.ID{}, businesses.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return businesses.ID{}, businesseshttp.ErrUnauthenticated
		}
		return businesses.ID{}, businesses.ErrUnavailable
	}
	var id businesses.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

type identityOffersSessions struct {
	sessions *identity.Sessions
}

func (s identityOffersSessions) Resolve(ctx context.Context, rawToken string) (offers.ID, error) {
	if s.sessions == nil {
		return offers.ID{}, offers.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return offers.ID{}, offershttp.ErrUnauthenticated
		}
		return offers.ID{}, offers.ErrUnavailable
	}
	var id offers.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

type identityTransactionsSessions struct {
	sessions *identity.Sessions
}

func (s identityTransactionsSessions) Resolve(ctx context.Context, rawToken string) (transactions.ID, error) {
	if s.sessions == nil {
		return transactions.ID{}, transactions.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return transactions.ID{}, transactionshttp.ErrUnauthenticated
		}
		return transactions.ID{}, transactions.ErrUnavailable
	}
	var id transactions.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

type identityPaymentsSessions struct {
	sessions *identity.Sessions
}

func (s identityPaymentsSessions) Resolve(ctx context.Context, rawToken string) (payments.ID, error) {
	if s.sessions == nil {
		return payments.ID{}, payments.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return payments.ID{}, paymentshttp.ErrUnauthenticated
		}
		return payments.ID{}, payments.ErrUnavailable
	}
	var id payments.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

type identityDeliveriesSessions struct {
	sessions *identity.Sessions
}

func (s identityDeliveriesSessions) Resolve(ctx context.Context, rawToken string) (deliveries.ID, error) {
	if s.sessions == nil {
		return deliveries.ID{}, deliveries.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return deliveries.ID{}, deliverieshttp.ErrUnauthenticated
		}
		return deliveries.ID{}, deliveries.ErrUnavailable
	}
	var id deliveries.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

type identityDisputesSessions struct {
	sessions *identity.Sessions
}

func (s identityDisputesSessions) Resolve(ctx context.Context, rawToken string) (disputes.ID, error) {
	if s.sessions == nil {
		return disputes.ID{}, disputes.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return disputes.ID{}, disputeshttp.ErrUnauthenticated
		}
		return disputes.ID{}, disputes.ErrUnavailable
	}
	var id disputes.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func newModerationHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions, staffAuth staffauthcontracts.Authorizer) (*moderationhttp.Handler, *moderationhttp.StaffHandler, error) {
	listingSvc, err := listings.NewService(listings.NewPostgresStore(pool), nil, nil)
	if err != nil {
		return nil, nil, err
	}
	ob, err := outbox.New(outbox.NewPostgresStore(pool), outbox.Policy{
		BatchSize:         cfg.OutboxBatchSize,
		Lease:             cfg.OutboxLease,
		BackoffBase:       cfg.OutboxRetryBase,
		BackoffMultiplier: cfg.OutboxRetryMultiplier,
		BackoffCap:        cfg.OutboxRetryCap,
		Jitter:            cfg.OutboxRetryJitter,
	}, nil)
	if err != nil {
		return nil, nil, err
	}
	listingSvc.SetOutbox(listings.PoolTransactor{Pool: pool}, ob)
	profileSvc, err := publicprofile.NewService(publicprofile.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, nil, err
	}
	profiles, err := publicprofile.NewResolver(profileSvc)
	if err != nil {
		return nil, nil, err
	}
	svc, err := moderation.NewService(
		moderation.NewPostgresStore(pool),
		listingSvc,
		profiles,
		moderation.DefaultPolicy(),
		nil,
	)
	if err != nil {
		return nil, nil, err
	}
	svc.SetOutbox(ob)
	h, err := moderationhttp.New(identityModerationSessions{sessions: sessions}, svc, cfg.WebAuthnRPOrigins)
	if err != nil {
		return nil, nil, err
	}
	var staff *moderationhttp.StaffHandler
	if staffAuth != nil {
		staff, err = moderationhttp.NewStaff(staffAuth, svc)
		if err != nil {
			return nil, nil, err
		}
	}
	return h, staff, nil
}

// Staff Moderation/Dispute HTTP is registered only when a StaffIdentityProvider
// adapter is wired. Missing STAFF_IDP keeps routes unavailable. Complete STAFF_IDP
// without a vendor adapter fails closed. STAFF_DEV_IDP is development/test only
// and cannot activate in staging/production. There is no consumer-session fallback.

type identityModerationSessions struct {
	sessions *identity.Sessions
}

func (s identityModerationSessions) Resolve(ctx context.Context, rawToken string) (moderation.ID, error) {
	if s.sessions == nil {
		return moderation.ID{}, moderation.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return moderation.ID{}, moderationhttp.ErrUnauthenticated
		}
		return moderation.ID{}, moderation.ErrUnavailable
	}
	var id moderation.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func newReviewAggregatesHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions) (*reviewaggregateshttp.Handler, error) {
	projector, err := reviewaggregates.NewProjector(reviewaggregates.NewPostgresStore(pool))
	if err != nil {
		return nil, err
	}
	svc, err := reviewaggregates.NewService(projector)
	if err != nil {
		return nil, err
	}
	return reviewaggregateshttp.New(identityReviewAggregatesSessions{sessions: sessions}, svc, cfg.WebAuthnRPOrigins)
}

type identityReviewAggregatesSessions struct {
	sessions *identity.Sessions
}

func (s identityReviewAggregatesSessions) Resolve(ctx context.Context, rawToken string) (reviewaggregates.ID, error) {
	if s.sessions == nil {
		return reviewaggregates.ID{}, reviewaggregates.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return reviewaggregates.ID{}, reviewaggregateshttp.ErrUnauthenticated
		}
		return reviewaggregates.ID{}, reviewaggregates.ErrUnavailable
	}
	var id reviewaggregates.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func newReviewsHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions) (*reviewshttp.Handler, error) {
	ob, err := outbox.New(outbox.NewPostgresStore(pool), outbox.Policy{
		BatchSize:         cfg.OutboxBatchSize,
		Lease:             cfg.OutboxLease,
		BackoffBase:       cfg.OutboxRetryBase,
		BackoffMultiplier: cfg.OutboxRetryMultiplier,
		BackoffCap:        cfg.OutboxRetryCap,
		Jitter:            cfg.OutboxRetryJitter,
	}, nil)
	if err != nil {
		return nil, err
	}
	listingSvc, err := listings.NewService(listings.NewPostgresStore(pool), nil, nil)
	if err != nil {
		return nil, err
	}
	svc, err := reviews.NewService(
		reviews.NewPostgresStore(pool),
		verified.NewInteractions(verified.NewPostgresStore(pool)),
		listingSvc,
		ob,
		reviews.DefaultPolicy(),
		nil,
	)
	if err != nil {
		return nil, err
	}
	return reviewshttp.New(identityReviewsSessions{sessions: sessions}, svc, cfg.WebAuthnRPOrigins)
}

type identityReviewsSessions struct {
	sessions *identity.Sessions
}

func (s identityReviewsSessions) Resolve(ctx context.Context, rawToken string) (reviews.ID, error) {
	if s.sessions == nil {
		return reviews.ID{}, reviews.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return reviews.ID{}, reviewshttp.ErrUnauthenticated
		}
		return reviews.ID{}, reviews.ErrUnavailable
	}
	var id reviews.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func newMasterDataHTTP(pool *db.Pool) (*masterdatahttp.Handler, error) {
	svc, err := masterdata.NewService(masterdata.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	return masterdatahttp.New(svc)
}

func newSearchHTTP(pool *db.Pool) (*searchhttp.Handler, error) {
	svc, err := search.NewService(search.NewPostgresStore(pool))
	if err != nil {
		return nil, err
	}
	return searchhttp.New(svc)
}

func newFavoritesHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions) (*favoriteshttp.Handler, error) {
	listingSvc, err := listings.NewService(listings.NewPostgresStore(pool), nil, nil)
	if err != nil {
		return nil, err
	}
	svc, err := favorites.NewService(favorites.NewPostgresStore(pool), listingSvc, nil)
	if err != nil {
		return nil, err
	}
	return favoriteshttp.New(identityFavoritesSessions{sessions: sessions}, svc, cfg.WebAuthnRPOrigins)
}

type identityFavoritesSessions struct {
	sessions *identity.Sessions
}

func (s identityFavoritesSessions) Resolve(ctx context.Context, rawToken string) (favorites.ID, error) {
	if s.sessions == nil {
		return favorites.ID{}, favorites.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return favorites.ID{}, favoriteshttp.ErrUnauthenticated
		}
		return favorites.ID{}, favorites.ErrUnavailable
	}
	var id favorites.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func newTrustHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions) (*trusthttp.Handler, error) {
	projector, err := trust.NewProjector(trust.NewPostgresStore(pool), trust.DefaultLevelPolicy())
	if err != nil {
		return nil, err
	}
	svc, err := trust.NewService(projector)
	if err != nil {
		return nil, err
	}
	profileSvc, err := publicprofile.NewService(publicprofile.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	profiles, err := publicprofile.NewResolver(profileSvc)
	if err != nil {
		return nil, err
	}
	return trusthttp.New(identityTrustSessions{sessions: sessions}, svc, profiles, cfg.WebAuthnRPOrigins)
}

type identityTrustSessions struct {
	sessions *identity.Sessions
}

func (s identityTrustSessions) Resolve(ctx context.Context, rawToken string) (trust.ID, error) {
	if s.sessions == nil {
		return trust.ID{}, trust.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return trust.ID{}, trusthttp.ErrUnauthenticated
		}
		return trust.ID{}, trust.ErrUnavailable
	}
	var id trust.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func newVerifiedHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions) (*verifiedhttp.Handler, error) {
	listingSvc, err := listings.NewService(listings.NewPostgresStore(pool), nil, nil)
	if err != nil {
		return nil, err
	}
	ob, err := outbox.New(outbox.NewPostgresStore(pool), outbox.Policy{
		BatchSize:         cfg.OutboxBatchSize,
		Lease:             cfg.OutboxLease,
		BackoffBase:       cfg.OutboxRetryBase,
		BackoffMultiplier: cfg.OutboxRetryMultiplier,
		BackoffCap:        cfg.OutboxRetryCap,
		Jitter:            cfg.OutboxRetryJitter,
	}, nil)
	if err != nil {
		return nil, err
	}
	svc, err := verified.NewService(verified.NewPostgresStore(pool), listingSvc, ob, verified.DefaultPolicy(), nil)
	if err != nil {
		return nil, err
	}
	return verifiedhttp.New(identityVerifiedSessions{sessions: sessions}, svc, cfg.WebAuthnRPOrigins)
}

type identityVerifiedSessions struct {
	sessions *identity.Sessions
}

func (s identityVerifiedSessions) Resolve(ctx context.Context, rawToken string) (verified.ID, error) {
	if s.sessions == nil {
		return verified.ID{}, verified.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return verified.ID{}, verifiedhttp.ErrUnauthenticated
		}
		return verified.ID{}, verified.ErrUnavailable
	}
	var id verified.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func newMessagingHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions) (*messaginghttp.Handler, error) {
	listingSvc, err := listings.NewService(listings.NewPostgresStore(pool), nil, nil)
	if err != nil {
		return nil, err
	}
	svc, err := messaging.NewService(messaging.NewPostgresStore(pool), listingSvc, nil)
	if err != nil {
		return nil, err
	}
	return messaginghttp.New(identityMessagingSessions{sessions: sessions}, svc, cfg.WebAuthnRPOrigins)
}

type identityMessagingSessions struct {
	sessions *identity.Sessions
}

func (s identityMessagingSessions) Resolve(ctx context.Context, rawToken string) (messaging.ID, error) {
	if s.sessions == nil {
		return messaging.ID{}, messaging.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return messaging.ID{}, messaginghttp.ErrUnauthenticated
		}
		return messaging.ID{}, messaging.ErrUnavailable
	}
	var id messaging.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func newSavedSearchHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions) (*savedsearchhttp.Handler, error) {
	svc, err := savedsearch.NewService(savedsearch.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	return savedsearchhttp.New(identitySavedSearchSessions{sessions: sessions}, svc, cfg.WebAuthnRPOrigins)
}

type identitySavedSearchSessions struct {
	sessions *identity.Sessions
}

func (s identitySavedSearchSessions) Resolve(ctx context.Context, rawToken string) (savedsearch.ID, error) {
	if s.sessions == nil {
		return savedsearch.ID{}, savedsearch.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return savedsearch.ID{}, savedsearchhttp.ErrUnauthenticated
		}
		return savedsearch.ID{}, savedsearch.ErrUnavailable
	}
	var id savedsearch.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func newListingsHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions, objects media.ObjectStorage) (*listingshttp.Handler, error) {
	mdSvc, err := masterdata.NewService(masterdata.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	listingSvc, err := listings.NewService(listings.NewPostgresStore(pool), masterdata.NewPublishedForms(mdSvc), nil)
	if err != nil {
		return nil, err
	}
	eidsPolicy := masterdata.NewEIDSPolicy(mdSvc)
	eidsSvc, err := eids.NewService(eids.NewPostgresStore(pool), listingSvc, eidsPolicy, eidsadapter.Unconfigured{}, nil)
	if err != nil {
		return nil, err
	}
	ob, err := outbox.New(outbox.NewPostgresStore(pool), outbox.Policy{
		BatchSize:         cfg.OutboxBatchSize,
		Lease:             cfg.OutboxLease,
		BackoffBase:       cfg.OutboxRetryBase,
		BackoffMultiplier: cfg.OutboxRetryMultiplier,
		BackoffCap:        cfg.OutboxRetryCap,
		Jitter:            cfg.OutboxRetryJitter,
	}, nil)
	if err != nil {
		return nil, err
	}
	listingSvc.SetOutbox(listings.PoolTransactor{Pool: pool}, ob)
	locSvc, err := location.NewService(location.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	locSvc.SetOutbox(ob)
	var listingMedia mediacontracts.ListingMedia
	var publicMedia mediacontracts.PublicListingMedia
	if objects != nil {
		mediaSvc, err := media.NewService(media.NewPostgresStore(pool), objects, nil)
		if err != nil {
			return nil, err
		}
		listingMedia = media.NewListingMedia(mediaSvc)
		publicMedia = media.NewPublicListingMedia(mediaSvc)
	}
	orch, err := listings.NewDraftOrchestrator(listingSvc, location.NewGeo(locSvc), listingMedia, listings.PoolTransactor{Pool: pool})
	if err != nil {
		return nil, err
	}
	profileSvc, err := publicprofile.NewService(publicprofile.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	profiles, err := publicprofile.NewResolver(profileSvc)
	if err != nil {
		return nil, err
	}
	h, err := listingshttp.New(identityListingsSessions{sessions: sessions}, orch, listingSvc, cfg.WebAuthnRPOrigins, location.NewListingLocations(locSvc), publicMedia, profiles)
	if err != nil {
		return nil, err
	}
	h.SetEIDS(eidsPolicy, eids.NewPublishGate(eidsSvc), eids.NewOwnerAPI(eidsSvc))
	return h, nil
}

type identityListingsSessions struct {
	sessions *identity.Sessions
}

func (s identityListingsSessions) Resolve(ctx context.Context, rawToken string) (listings.ID, error) {
	if s.sessions == nil {
		return listings.ID{}, listings.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return listings.ID{}, listingshttp.ErrUnauthenticated
		}
		return listings.ID{}, listings.ErrUnavailable
	}
	var id listings.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func newMediaHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions, objects media.ObjectStorage, maxUploadBytes int64) (*mediahttp.Handler, error) {
	var svc *media.Service
	if objects != nil {
		store := media.NewPostgresStore(pool)
		created, err := media.NewService(store, objects, nil)
		if err != nil {
			return nil, err
		}
		created.SetMaxUploadBytes(maxUploadBytes)
		ob, err := outbox.New(outbox.NewPostgresStore(pool), outbox.Policy{
			BatchSize:         cfg.OutboxBatchSize,
			Lease:             cfg.OutboxLease,
			BackoffBase:       cfg.OutboxRetryBase,
			BackoffMultiplier: cfg.OutboxRetryMultiplier,
			BackoffCap:        cfg.OutboxRetryCap,
			Jitter:            cfg.OutboxRetryJitter,
		}, nil)
		if err != nil {
			return nil, err
		}
		created.SetOutbox(store, ob)
		created.SetProcessingPolicy(media.ProcessingPolicy{
			RequireMalwareScan: cfg.MediaMalwareScanRequired,
			RequireModeration:  cfg.MediaImageModerationRequired,
			MaxBytes:           maxUploadBytes,
		})
		svc = created
	}
	listingSvc, err := listings.NewService(listings.NewPostgresStore(pool), nil, nil)
	if err != nil {
		return nil, err
	}
	return mediahttp.New(identityMediaSessions{sessions: sessions}, svc, listingSvc, cfg.WebAuthnRPOrigins)
}

type identityMediaSessions struct {
	sessions *identity.Sessions
}

func (s identityMediaSessions) Resolve(ctx context.Context, rawToken string) (media.ID, error) {
	if s.sessions == nil {
		return media.ID{}, media.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return media.ID{}, mediahttp.ErrUnauthenticated
		}
		return media.ID{}, media.ErrUnavailable
	}
	var id media.ID
	copy(id[:], session.UserID[:])
	return id, nil
}

func openCache(ctx context.Context, valkeyURL string) (*cache.Client, health.CheckFunc, error) {
	if valkeyURL == "" {
		return nil, nil, nil
	}
	client, err := cache.Open(ctx, valkeyURL)
	if err != nil {
		return nil, nil, err
	}
	if err := client.Ping(ctx); err != nil {
		log.Print("cache not ready at startup")
	}
	return client, client.Ping, nil
}

func composeReady(checks ...health.CheckFunc) health.CheckFunc {
	return func(ctx context.Context) error {
		for _, check := range checks {
			if check == nil {
				continue
			}
			if err := check(ctx); err != nil {
				return err
			}
		}
		return nil
	}
}

func newIdentityHTTP(pool *db.Pool, cfg config.Config, cacheClient *cache.Client, sessions *identity.Sessions) (*httpapi.Handler, error) {
	protector, err := identity.NewMaterialProtectorFromDecodedKeys(cfg.MaterialKeys.ActiveID, cfg.MaterialKeys.Keys)
	if err != nil {
		return nil, err
	}
	if sessions == nil {
		return nil, identity.ErrUnavailable
	}
	store := identity.NewPostgresStore(pool)
	ceremonies, err := identity.NewCeremonies(store, identity.CeremonyPolicy{TTL: cfg.WebAuthnCeremonyTTL}, nil)
	if err != nil {
		return nil, err
	}
	passkeys, err := identity.NewPasskeys(store, nil)
	if err != nil {
		return nil, err
	}
	wa, err := identity.NewWebAuthn(identity.WebAuthnConfig{
		RPDisplayName: cfg.WebAuthnRPDisplayName,
		RPID:          cfg.WebAuthnRPID,
		RPOrigins:     cfg.WebAuthnRPOrigins,
	})
	if err != nil {
		return nil, err
	}
	auth, err := identity.NewAuthentication(wa, passkeys, ceremonies, store, nil)
	if err != nil {
		return nil, err
	}
	identifiers, err := identity.NewIdentifiers(store, nil)
	if err != nil {
		return nil, err
	}
	passwords, err := identity.NewPasswords(store, identity.PasswordPolicy{
		MemoryKiB:   19456,
		Iterations:  2,
		Parallelism: 1,
		SaltLen:     16,
		KeyLen:      32,
	}, nil)
	if err != nil {
		return nil, err
	}
	limiter, err := identity.NewIssuanceLimiter(cacheClient, identity.IssuanceLimitPolicy{
		DestinationMax:    cfg.IssueDestMax,
		DestinationWindow: cfg.IssueDestWindow,
		IPMax:             cfg.IssueIPMax,
		IPWindow:          cfg.IssueIPWindow,
	})
	if err != nil {
		return nil, err
	}
	challenges, err := identity.NewChallenges(store, identity.VerificationChallengePolicy{
		TTL:            cfg.ChallengeTTL,
		MaxAttempts:    cfg.ChallengeMaxAttempts,
		PhoneOTPDigits: cfg.ChallengePhoneOTPDigits,
	}, limiter, protector, nil)
	if err != nil {
		return nil, err
	}
	ob, err := outbox.New(outbox.NewPostgresStore(pool), outbox.Policy{
		BatchSize:         cfg.OutboxBatchSize,
		Lease:             cfg.OutboxLease,
		BackoffBase:       cfg.OutboxRetryBase,
		BackoffMultiplier: cfg.OutboxRetryMultiplier,
		BackoffCap:        cfg.OutboxRetryCap,
		Jitter:            cfg.OutboxRetryJitter,
	}, nil)
	if err != nil {
		return nil, err
	}
	proofs, err := identity.NewSignupProofs(store, identity.SignupProofPolicy{TTL: cfg.SignupProofTTL}, nil)
	if err != nil {
		return nil, err
	}
	signup, err := identity.NewSignupVerification(challenges, store, store, ob, proofs)
	if err != nil {
		return nil, err
	}
	accounts, err := identity.NewAccountCreation(proofs, passwords, store, store, nil)
	if err != nil {
		return nil, err
	}
	resetProofs, err := identity.NewResetProofs(store, identity.ResetProofPolicy{TTL: cfg.SignupProofTTL}, nil)
	if err != nil {
		return nil, err
	}
	reset, err := identity.NewPasswordReset(challenges, identifiers, store, ob, resetProofs, passwords, store, sessions)
	if err != nil {
		return nil, err
	}
	registration, err := identity.NewRegistration(wa, passkeys, ceremonies, nil)
	if err != nil {
		return nil, err
	}
	challengePolicy, verifier, err := newHumanChallenge(cfg)
	if err != nil {
		return nil, err
	}
	guard, err := httpapi.NewAbuseGuard(cacheClient, httpapi.AuthRateLimit{
		IPMaxAttempts:           cfg.AuthIPMaxAttempts,
		IPWindow:                cfg.AuthIPWindow,
		PasswordUserMaxAttempts: cfg.AuthPasswordUserMaxAttempts,
		PasswordUserWindow:      cfg.AuthPasswordUserWindow,
		TargetMaxAttempts:       cfg.AuthTargetMaxAttempts,
		TargetWindow:            cfg.AuthTargetWindow,
		CompleteMaxAttempts:     cfg.AuthCompleteMaxAttempts,
		CompleteWindow:          cfg.AuthCompleteWindow,
		SensitiveMaxAttempts:    cfg.AuthSensitiveMaxAttempts,
		SensitiveWindow:         cfg.AuthSensitiveWindow,
		Challenge:               challengePolicy,
		Verifier:                verifier,
	}, httpx.ClientIP(cfg.TrustedProxies))
	if err != nil {
		return nil, err
	}
	return httpapi.New(auth, sessions, identifiers, passwords, signup, accounts, registration, reset, cfg.WebAuthnRPOrigins, guard)
}

func newPublicProfileHTTP(pool *db.Pool, cfg config.Config, sessions *identity.Sessions) (*publicprofile.Handler, error) {
	if sessions == nil {
		return nil, identity.ErrUnavailable
	}
	svc, err := publicprofile.NewService(publicprofile.NewPostgresStore(pool), nil)
	if err != nil {
		return nil, err
	}
	return publicprofile.NewHandler(identityPublicProfileSessions{sessions: sessions}, svc, cfg.WebAuthnRPOrigins)
}

type identityPublicProfileSessions struct {
	sessions *identity.Sessions
}

func (s identityPublicProfileSessions) Resolve(ctx context.Context, rawToken string) (identity.ID, error) {
	if s.sessions == nil {
		return identity.ID{}, identity.ErrUnavailable
	}
	session, err := s.sessions.Resolve(ctx, rawToken)
	if err != nil {
		if identity.Classify(err) == identity.FailureUnauthenticated {
			return identity.ID{}, publicprofile.ErrUnauthenticated
		}
		return identity.ID{}, identity.ErrUnavailable
	}
	return session.UserID, nil
}

func newHumanChallenge(cfg config.Config) (identity.HumanChallengePolicy, identity.HumanChallenge, error) {
	required := make(map[identity.AuthOperation]struct{}, len(cfg.HumanChallenge.Operations))
	for _, raw := range cfg.HumanChallenge.Operations {
		op, ok := identity.ParseAuthOperation(raw)
		if !ok {
			return identity.HumanChallengePolicy{}, nil, identity.ErrInvalidAbusePolicy
		}
		required[op] = struct{}{}
	}
	policy := identity.HumanChallengePolicy{
		Provider:  cfg.HumanChallenge.Provider,
		Required:  required,
		Hostname:  cfg.HumanChallenge.Hostname,
		ReplayTTL: cfg.HumanChallenge.ReplayTTL,
	}
	if err := policy.Validate(); err != nil {
		return identity.HumanChallengePolicy{}, nil, err
	}
	verifier, err := identity.NewHumanChallengeVerifier(cfg.HumanChallenge.Provider)
	if err != nil {
		return identity.HumanChallengePolicy{}, nil, err
	}
	return policy, verifier, nil
}
