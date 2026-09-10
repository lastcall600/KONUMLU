package location

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
)

type locationEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

// Service is listing-geo orchestration. Search, maps, and reverse geocoding are out of scope.
type Service struct {
	store  listingLocationStore
	now    func() time.Time
	outbox locationEnqueuer
}

func NewService(store listingLocationStore, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, now: now}, nil
}

// SetOutbox enables listing-geo change events. Nil enqueuer disables emit.
func (s *Service) SetOutbox(enqueuer locationEnqueuer) {
	if s == nil {
		return
	}
	s.outbox = enqueuer
}

func (s *Service) SetListingLocation(ctx context.Context, scope ListingWriteScope, coords Coordinates, catalogLocationID *ID) (ListingLocation, error) {
	if s == nil || s.store == nil {
		return ListingLocation{}, errStoreRequired
	}
	if scope.ListingID.IsZero() {
		return ListingLocation{}, errZeroID
	}
	loc, err := NewListingLocation(scope.ListingID, coords, catalogLocationID, s.now().UTC())
	if err != nil {
		return ListingLocation{}, err
	}
	if err := s.store.Upsert(ctx, loc); err != nil {
		return ListingLocation{}, mapStoreErr(err)
	}
	stored, err := s.store.GetByListingID(ctx, scope.ListingID)
	if err != nil {
		return ListingLocation{}, mapStoreErr(err)
	}
	if err := s.emitListingChanged(ctx, stored); err != nil {
		return ListingLocation{}, err
	}
	return stored, nil
}

func (s *Service) GetByListingID(ctx context.Context, listingID ID) (ListingLocation, error) {
	if s == nil || s.store == nil {
		return ListingLocation{}, errStoreRequired
	}
	if listingID.IsZero() {
		return ListingLocation{}, errZeroID
	}
	loc, err := s.store.GetByListingID(ctx, listingID)
	if err != nil {
		return ListingLocation{}, mapStoreErr(err)
	}
	return loc, nil
}

func (s *Service) emitListingChanged(ctx context.Context, loc ListingLocation) error {
	if s == nil || s.outbox == nil {
		return nil
	}
	ev, err := encodeListingChangedEvent(loc.ListingID, loc.UpdatedAt)
	if err != nil {
		return err
	}
	if _, err := s.outbox.Enqueue(ctx, nil, ev); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func (s *Service) DeleteByListingID(ctx context.Context, listingID ID) error {
	if s == nil || s.store == nil {
		return errStoreRequired
	}
	if listingID.IsZero() {
		return errZeroID
	}
	if err := s.store.DeleteByListingID(ctx, listingID); err != nil {
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
	if errors.Is(err, errUnavailable) || errors.Is(err, db.ErrUnavailable) {
		return errUnavailable
	}
	if errors.Is(err, errStoreRequired) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidCoordinate) || errors.Is(err, errInvalidLatitude) ||
		errors.Is(err, errInvalidLongitude) || errors.Is(err, errInvalidLocation) {
		return err
	}
	return errUnavailable
}
