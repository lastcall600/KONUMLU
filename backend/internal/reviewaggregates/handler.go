package reviewaggregates

import (
	"context"

	"backend/internal/platform/outbox"
	reviewscontracts "backend/internal/reviews/contracts"
)

// ProjectionHandler consumes reviews.verified.created v1 and rebuilds
// derived listing/provider aggregates. It does not import reviews implementation.
type ProjectionHandler struct {
	projector *Projector
}

func NewProjectionHandler(projector *Projector) (*ProjectionHandler, error) {
	if projector == nil {
		return nil, errStoreRequired
	}
	return &ProjectionHandler{projector: projector}, nil
}

func (h *ProjectionHandler) Handle(ctx context.Context, event outbox.Event) error {
	if h == nil || h.projector == nil {
		return errStoreRequired
	}
	verified, err := verifiedFromOutbox(event)
	if err != nil {
		return err
	}
	return h.projector.Apply(ctx, verified)
}

func KnownReviewAggregateEvent(eventType string, version int) bool {
	return eventType == reviewscontracts.EventTypeVerifiedCreated && version == reviewscontracts.EventVersion
}

var _ outbox.Handler = (*ProjectionHandler)(nil)
