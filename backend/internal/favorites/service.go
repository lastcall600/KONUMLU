package favorites

import (
	"context"
	"errors"
	"time"

	listingcontracts "backend/internal/listings/contracts"
)

type Service struct {
	store    favoriteStore
	listings listingcontracts.SearchSource
	now      func() time.Time
}

func NewService(store favoriteStore, listings listingcontracts.SearchSource, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if listings == nil {
		return nil, errListingsReq
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, listings: listings, now: now}, nil
}

func (s *Service) Add(ctx context.Context, userID, listingID ID) error {
	if s == nil || s.store == nil {
		return errStoreRequired
	}
	if userID.IsZero() || listingID.IsZero() {
		return errZeroID
	}
	if err := s.requirePublicListing(ctx, listingID); err != nil {
		return err
	}
	fav := Favorite{UserID: userID, ListingID: listingID, CreatedAt: s.now().UTC()}
	if err := s.store.Add(ctx, fav); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func (s *Service) Remove(ctx context.Context, userID, listingID ID) error {
	if s == nil || s.store == nil {
		return errStoreRequired
	}
	if userID.IsZero() || listingID.IsZero() {
		return errZeroID
	}
	if err := s.store.Remove(ctx, userID, listingID); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func (s *Service) IsFavorited(ctx context.Context, userID, listingID ID) (bool, error) {
	if s == nil || s.store == nil {
		return false, errStoreRequired
	}
	if userID.IsZero() || listingID.IsZero() {
		return false, errZeroID
	}
	_, err := s.store.Get(ctx, userID, listingID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return false, nil
		}
		return false, mapStoreErr(err)
	}
	return true, nil
}

func (s *Service) ListVisible(ctx context.Context, userID ID) ([]Favorite, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if userID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListByUser(ctx, userID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	out := make([]Favorite, 0, len(rows))
	for _, fav := range rows {
		ok, err := s.listingIsPublic(ctx, fav.ListingID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, fav)
	}
	return out, nil
}

func (s *Service) requirePublicListing(ctx context.Context, listingID ID) error {
	ok, err := s.listingIsPublic(ctx, listingID)
	if err != nil {
		return err
	}
	if !ok {
		return errNotFound
	}
	return nil
}

func (s *Service) listingIsPublic(ctx context.Context, listingID ID) (bool, error) {
	if s.listings == nil {
		return false, errListingsReq
	}
	snap, err := s.listings.GetListingSnapshot(ctx, listingcontracts.ID(listingID))
	if err != nil {
		if errors.Is(err, listingcontracts.ErrNotFound) || errors.Is(err, listingcontracts.ErrForbidden) {
			return false, nil
		}
		if errors.Is(err, listingcontracts.ErrZeroID) {
			return false, errZeroID
		}
		return false, errUnavailable
	}
	return snap.PubliclyVisible(), nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errZeroID) || errors.Is(err, errStoreRequired) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
