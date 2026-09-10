package reviewaggregates

import (
	"context"
	"time"
)

type projectionStore interface {
	ApplyVerified(ctx context.Context, event VerifiedReview, now time.Time) error
	GetListingAccuracy(ctx context.Context, listingID ID) (RatingSummary, error)
	GetProviderService(ctx context.Context, providerUserID ID) (RatingSummary, error)
}
