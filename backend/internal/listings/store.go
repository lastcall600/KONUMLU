package listings

import (
	"context"
	"time"
)

type listingStore interface {
	Create(ctx context.Context, listing Listing) error
	Get(ctx context.Context, id ID) (Listing, error)
	Update(ctx context.Context, listing Listing, expectedUpdatedAt time.Time) error
	ListByOwner(ctx context.Context, ownerUserID ID) ([]Listing, error)
}
