package trust

import (
	"context"

	"backend/internal/platform/outbox"
	reviewscontracts "backend/internal/reviews/contracts"
	verifiedcontracts "backend/internal/verified/contracts"
)

// ProjectionHandler consumes verified.interaction.completed v1 and
// reviews.verified.created v1. It does not import verified or reviews
// implementation packages. Review signals update visible counters only;
// they never change TrustLevel.
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
	switch {
	case event.EventType == verifiedcontracts.EventTypeInteractionCompleted && event.EventVersion == verifiedcontracts.EventVersion:
		completed, err := completedFromOutbox(event)
		if err != nil {
			return err
		}
		return h.projector.Apply(ctx, completed)
	case event.EventType == reviewscontracts.EventTypeVerifiedCreated && event.EventVersion == reviewscontracts.EventVersion:
		verified, err := verifiedReviewFromOutbox(event)
		if err != nil {
			return err
		}
		return h.projector.ApplyVerifiedReview(ctx, verified)
	default:
		return errInvalidEvent
	}
}

func KnownTrustEvent(eventType string, version int) bool {
	return (eventType == verifiedcontracts.EventTypeInteractionCompleted && version == verifiedcontracts.EventVersion) ||
		(eventType == reviewscontracts.EventTypeVerifiedCreated && version == reviewscontracts.EventVersion)
}

var _ outbox.Handler = (*ProjectionHandler)(nil)
