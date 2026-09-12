package media

import (
	"context"
	"time"

	"backend/internal/platform/outbox"
)

type assetStore interface {
	Create(ctx context.Context, asset Asset) error
	Get(ctx context.Context, id ID) (Asset, error)
	Update(ctx context.Context, asset Asset, expectedUpdatedAt time.Time) error
	UpdateInTx(ctx context.Context, exec outbox.Execer, asset Asset, expectedUpdatedAt time.Time) error
	ListByListing(ctx context.Context, listingID ID) ([]Asset, error)
	ListReclaimable(ctx context.Context, now time.Time, pendingAge, rejectedAge time.Duration, limit int) ([]Asset, error)
}
