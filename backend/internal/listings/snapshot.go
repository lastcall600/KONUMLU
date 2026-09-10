package listings

import (
	"context"

	"backend/internal/listings/contracts"
)

func (s *Service) GetListingSnapshot(ctx context.Context, listingID contracts.ID) (contracts.ListingSnapshot, error) {
	listing, err := s.Get(ctx, ID(listingID))
	if err != nil {
		return contracts.ListingSnapshot{}, mapOwnershipErr(err)
	}
	amount, currency := clonePrice(listing.PriceAmount, listing.PriceCurrency)
	snap := contracts.ListingSnapshot{
		ID:                    contracts.ID(listing.ID),
		Status:                string(listing.Status),
		ModerationState:       string(listing.ModerationState.Normalized()),
		CategoryID:            contracts.ID(listing.CategoryID),
		CategorySchemaVersion: listing.CategorySchemaVersion,
		Title:                 listing.Title,
		Description:           listing.Description,
		PriceAmount:           amount,
		PriceCurrency:         currency,
		Attributes:            cloneAttributes(listing.Attributes),
		UpdatedAt:             listing.UpdatedAt,
	}
	if listing.PublishedAt != nil {
		t := *listing.PublishedAt
		snap.PublishedAt = &t
	}
	return snap, nil
}

var _ contracts.SearchSource = (*Service)(nil)
var _ contracts.ModerationEnforcement = (*Service)(nil)
