package listings

import (
	"context"
	"errors"

	"backend/internal/listings/contracts"
)

var _ contracts.Ownership = (*Service)(nil)
var _ contracts.EIDSSubject = (*Service)(nil)

func (s *Service) ResolveListingOwner(ctx context.Context, listingID contracts.ID) (contracts.ListingRef, error) {
	listing, err := s.Get(ctx, ID(listingID))
	if err != nil {
		return contracts.ListingRef{}, mapOwnershipErr(err)
	}
	return contracts.ListingRef{
		ID:              contracts.ID(listing.ID),
		OwnerUserID:     contracts.ID(listing.OwnerUserID),
		Status:          string(listing.Status),
		ModerationState: string(listing.ModerationState.Normalized()),
	}, nil
}

func (s *Service) ResolveListingEIDSSubject(ctx context.Context, listingID contracts.ID) (contracts.ListingEIDSSubject, error) {
	listing, err := s.Get(ctx, ID(listingID))
	if err != nil {
		return contracts.ListingEIDSSubject{}, mapOwnershipErr(err)
	}
	return contracts.ListingEIDSSubject{
		ID:          contracts.ID(listing.ID),
		OwnerUserID: contracts.ID(listing.OwnerUserID),
		CategoryID:  contracts.ID(listing.CategoryID),
	}, nil
}

func (s *Service) AssertListingOwnedBy(ctx context.Context, listingID, userID contracts.ID) error {
	if listingID.IsZero() || userID.IsZero() {
		return contracts.ErrZeroID
	}
	ref, err := s.ResolveListingOwner(ctx, listingID)
	if err != nil {
		return err
	}
	if ref.OwnerUserID != userID {
		return contracts.ErrForbidden
	}
	return nil
}

func (s *Service) assertOwnedBy(ctx context.Context, listingID, userID ID) error {
	if listingID.IsZero() || userID.IsZero() {
		return errZeroID
	}
	listing, err := s.Get(ctx, listingID)
	if err != nil {
		return err
	}
	if listing.OwnerUserID != userID {
		return errForbidden
	}
	return nil
}

func mapOwnershipErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) {
		return contracts.ErrZeroID
	}
	if errors.Is(err, errNotFound) {
		return contracts.ErrNotFound
	}
	if errors.Is(err, errForbidden) {
		return contracts.ErrForbidden
	}
	if errors.Is(err, errConflict) {
		return contracts.ErrConflict
	}
	if errors.Is(err, errInvalidModeration) {
		return contracts.ErrInvalidModerationState
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, errStoreRequired) {
		return contracts.ErrUnavailable
	}
	return contracts.ErrUnavailable
}
