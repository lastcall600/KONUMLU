package reviews

import (
	"context"

	"backend/internal/platform/outbox"
)

type reviewStore interface {
	InsertReview(ctx context.Context, row Review, enqueue func(ctx context.Context, exec outbox.Execer) error) error
	GetByInteraction(ctx context.Context, verifiedInteractionID ID) (Review, error)
	ListForReviewer(ctx context.Context, reviewerUserID ID, limit int) ([]Review, error)
	ListForListing(ctx context.Context, listingID ID, cursor *listingCursor, limit int) ([]Review, error)
}
