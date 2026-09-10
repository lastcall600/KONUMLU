package offers

import (
	"context"
	"time"
)

type store interface {
	Create(ctx context.Context, offer Offer) error
	Get(ctx context.Context, id ID) (Offer, error)
	ListByNeed(ctx context.Context, needID ID) ([]Offer, error)
	ListByProvider(ctx context.Context, providerUserID ID) ([]Offer, error)
	FindSubmitted(ctx context.Context, needID, serviceID ID) (Offer, error)
	Update(ctx context.Context, offer Offer, expectedUpdatedAt time.Time) error
	AcceptExclusive(ctx context.Context, offerID, needID ID, now time.Time) (Offer, error)
}
