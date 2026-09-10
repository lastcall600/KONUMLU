package reviews

import (
	"encoding/json"
	"fmt"
	"time"

	"backend/internal/platform/outbox"
	reviewscontracts "backend/internal/reviews/contracts"
)

const reviewAggregateType = "review"

func encodeVerifiedCreated(row Review) (outbox.NewEvent, error) {
	payload, err := json.Marshal(reviewscontracts.VerifiedCreatedPayload{
		ReviewID:              row.ID.String(),
		VerifiedInteractionID: row.VerifiedInteractionID.String(),
		ListingID:             row.ListingID.String(),
		ReviewerUserID:        row.ReviewerUserID.String(),
		ProviderUserID:        row.ProviderUserID.String(),
		ListingAccuracy:       row.ListingAccuracy,
		ProviderService:       row.ProviderService,
		CreatedAt:             row.CreatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return outbox.NewEvent{}, errUnavailable
	}
	return outbox.NewEvent{
		EventType:      EventTypeVerifiedCreated,
		EventVersion:   EventVersion,
		AggregateType:  reviewAggregateType,
		AggregateID:    row.ID.String(),
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("%s:%s", EventTypeVerifiedCreated, row.VerifiedInteractionID.String()),
	}, nil
}
