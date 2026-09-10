package location

import "context"

type listingLocationStore interface {
	Upsert(ctx context.Context, loc ListingLocation) error
	GetByListingID(ctx context.Context, listingID ID) (ListingLocation, error)
	DeleteByListingID(ctx context.Context, listingID ID) error
}
