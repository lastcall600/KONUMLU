package listings

import (
	"context"
	"errors"
	"time"

	"backend/internal/listings/contracts"
	mdcontracts "backend/internal/masterdata/contracts"
	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
)

// Service is listing lifecycle orchestration. It does not implement EİDS.
type searchEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

// Service is listing lifecycle orchestration. It does not implement EİDS.
type Service struct {
	store  listingStore
	forms  mdcontracts.PublishedFormResolver
	now    func() time.Time
	tx     Transactor
	outbox searchEnqueuer
}

func NewService(store listingStore, forms mdcontracts.PublishedFormResolver, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, forms: forms, now: now}, nil
}

// SetOutbox enables transactional listing search events. Nil enqueuer disables emit.
func (s *Service) SetOutbox(tx Transactor, enqueuer searchEnqueuer) {
	if s == nil {
		return
	}
	s.tx = tx
	s.outbox = enqueuer
}

func (s *Service) CreateDraft(ctx context.Context, ownerUserID ID, content DraftContent) (Listing, error) {
	if s == nil || s.store == nil {
		return Listing{}, errStoreRequired
	}
	listing, err := NewDraft(ownerUserID, content, s.now().UTC())
	if err != nil {
		return Listing{}, err
	}
	if err := s.validateBoundAttributes(ctx, listing.CategoryID, listing.CategorySchemaVersion, listing.Attributes); err != nil {
		return Listing{}, err
	}
	if err := s.store.Create(ctx, listing); err != nil {
		return Listing{}, mapStoreErr(err)
	}
	return listing, nil
}

func (s *Service) Get(ctx context.Context, id ID) (Listing, error) {
	if s == nil || s.store == nil {
		return Listing{}, errStoreRequired
	}
	if id.IsZero() {
		return Listing{}, errZeroID
	}
	listing, err := s.store.Get(ctx, id)
	if err != nil {
		return Listing{}, mapStoreErr(err)
	}
	return listing, nil
}

// GetPublished returns a listing only when status is published.
// Non-public statuses map to not found so existence is not leaked.
func (s *Service) GetPublished(ctx context.Context, id ID) (Listing, error) {
	listing, err := s.Get(ctx, id)
	if err != nil {
		return Listing{}, err
	}
	if !listing.PubliclyReadable() {
		return Listing{}, errNotFound
	}
	return listing, nil
}

func (s *Service) ListByOwner(ctx context.Context, ownerUserID ID) ([]Listing, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if ownerUserID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListByOwner(ctx, ownerUserID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return rows, nil
}

func (s *Service) UpdateDraft(ctx context.Context, id ID, expectedUpdatedAt time.Time, content DraftContent) (Listing, error) {
	return s.mutate(ctx, id, expectedUpdatedAt, func(listing Listing, now time.Time) (Listing, error) {
		if content.CategoryID != listing.CategoryID || content.CategorySchemaVersion != listing.CategorySchemaVersion {
			return Listing{}, errInvalidContent
		}
		if err := s.validateBoundAttributes(ctx, listing.CategoryID, listing.CategorySchemaVersion, content.Attributes); err != nil {
			return Listing{}, err
		}
		return listing.UpdateDraft(content, now)
	})
}

func (s *Service) MarkReady(ctx context.Context, id ID, expectedUpdatedAt time.Time) (Listing, error) {
	return s.mutate(ctx, id, expectedUpdatedAt, func(listing Listing, now time.Time) (Listing, error) {
		return listing.MarkReady(now)
	})
}

func (s *Service) Archive(ctx context.Context, id ID, expectedUpdatedAt time.Time) (Listing, error) {
	return s.mutate(ctx, id, expectedUpdatedAt, func(listing Listing, now time.Time) (Listing, error) {
		return listing.Archive(now)
	})
}

func (s *Service) Publish(ctx context.Context, id ID, expectedUpdatedAt time.Time, eligible bool) (Listing, error) {
	return s.mutate(ctx, id, expectedUpdatedAt, func(listing Listing, now time.Time) (Listing, error) {
		return listing.Publish(now, eligible)
	})
}

func (s *Service) ApplyModerationState(ctx context.Context, in contracts.ApplyModerationInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	state, err := ParseModerationState(in.State)
	if err != nil {
		return mapContractApplyErr(err)
	}
	current, err := s.Get(ctx, ID(in.ListingID))
	if err != nil {
		return mapContractApplyErr(err)
	}
	_, err = s.mutate(ctx, ID(in.ListingID), current.UpdatedAt, func(listing Listing, now time.Time) (Listing, error) {
		return listing.ApplyModeration(state, now)
	})
	if err != nil {
		return mapContractApplyErr(err)
	}
	return nil
}

func (s *Service) ClearModerationState(ctx context.Context, listingID contracts.ID) error {
	if listingID.IsZero() {
		return contracts.ErrZeroID
	}
	current, err := s.Get(ctx, ID(listingID))
	if err != nil {
		return mapContractApplyErr(err)
	}
	_, err = s.mutate(ctx, ID(listingID), current.UpdatedAt, func(listing Listing, now time.Time) (Listing, error) {
		return listing.ClearModeration(now)
	})
	if err != nil {
		return mapContractApplyErr(err)
	}
	return nil
}

func mapContractApplyErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) {
		return contracts.ErrZeroID
	}
	if errors.Is(err, errNotFound) {
		return contracts.ErrNotFound
	}
	if errors.Is(err, errConflict) {
		return contracts.ErrConflict
	}
	if errors.Is(err, errInvalidModeration) {
		return contracts.ErrInvalidModerationState
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return contracts.ErrUnavailable
}

func (s *Service) validateBoundAttributes(ctx context.Context, categoryID ID, schemaVersion int64, attrs Attributes) error {
	if s.forms == nil {
		return errUnavailable
	}
	form, err := s.forms.ResolvePublishedForm(ctx, mdcontracts.ID(categoryID), schemaVersion)
	if err != nil {
		return mapMasterDataErr(err)
	}
	if form.CategoryID != mdcontracts.ID(categoryID) || form.SchemaVersion != schemaVersion {
		return errInvalidCategorySchema
	}
	return validateAttributesAgainstForm(attrs, form)
}

func mapMasterDataErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, mdcontracts.ErrZeroID) || errors.Is(err, mdcontracts.ErrNotFound) {
		return errInvalidCategorySchema
	}
	if errors.Is(err, mdcontracts.ErrUnavailable) {
		return errUnavailable
	}
	return errUnavailable
}

func (s *Service) mutate(ctx context.Context, id ID, expectedUpdatedAt time.Time, fn func(Listing, time.Time) (Listing, error)) (Listing, error) {
	if s == nil || s.store == nil {
		return Listing{}, errStoreRequired
	}
	if id.IsZero() {
		return Listing{}, errZeroID
	}
	current, err := s.store.Get(ctx, id)
	if err != nil {
		return Listing{}, mapStoreErr(err)
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return Listing{}, errConflict
	}
	next, err := fn(current, s.now().UTC())
	if err != nil {
		return Listing{}, err
	}
	apply := func(ctx context.Context) error {
		if err := s.store.Update(ctx, next, expectedUpdatedAt); err != nil {
			return mapStoreErr(err)
		}
		return s.emitLifecycle(ctx, current, next)
	}
	if s.outbox != nil && s.tx != nil && db.TxFrom(ctx) == nil {
		if err := s.runTx(ctx, apply); err != nil {
			return Listing{}, err
		}
		return next, nil
	}
	if err := apply(ctx); err != nil {
		return Listing{}, err
	}
	return next, nil
}

func (s *Service) emitLifecycle(ctx context.Context, previous, next Listing) error {
	if s == nil || s.outbox == nil {
		return nil
	}
	eventType, ok := lifecycleEventType(previous, next)
	if !ok {
		return nil
	}
	ev, err := encodeListingEvent(eventType, next.ID, next.UpdatedAt)
	if err != nil {
		return err
	}
	if _, err := s.outbox.Enqueue(ctx, nil, ev); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func (s *Service) runTx(ctx context.Context, fn func(context.Context) error) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return mapStoreErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx.Context(ctx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, errConflict) || errors.Is(err, db.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, db.ErrUnavailable) {
		return errUnavailable
	}
	if errors.Is(err, errStoreRequired) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidListing) || errors.Is(err, errInvalidStatus) ||
		errors.Is(err, errInvalidTransition) || errors.Is(err, errInvalidAttributes) ||
		errors.Is(err, errInvalidPrice) || errors.Is(err, errInvalidContent) ||
		errors.Is(err, errInvalidCategorySchema) ||
		errors.Is(err, errPublishNotEligible) || errors.Is(err, errForbidden) ||
		errors.Is(err, errInvalidModeration) {
		return err
	}
	return errUnavailable
}
