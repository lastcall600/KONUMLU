package listings

import (
	"encoding/json"
	"fmt"
	"time"

	"backend/internal/listings/contracts"
	"backend/internal/platform/outbox"
)

const listingAggregateType = "listing"

func encodeListingEvent(eventType string, listingID ID, at time.Time) (outbox.NewEvent, error) {
	payload, err := json.Marshal(contracts.ListingIDPayload{ListingID: listingID.String()})
	if err != nil {
		return outbox.NewEvent{}, errUnavailable
	}
	stamp := at.UTC().UnixNano()
	return outbox.NewEvent{
		EventType:      eventType,
		EventVersion:   contracts.EventVersion,
		AggregateType:  listingAggregateType,
		AggregateID:    listingID.String(),
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", eventType, listingID.String(), stamp),
	}, nil
}

func lifecycleEventType(previous, next Listing) (string, bool) {
	if next.Status == StatusPublished && previous.Status != StatusPublished {
		return contracts.EventTypePublished, true
	}
	if next.Status == StatusArchived && previous.Status != StatusArchived {
		return contracts.EventTypeArchived, true
	}
	if next.Status == StatusPublished && previous.Status == StatusPublished {
		return contracts.EventTypeUpdated, true
	}
	if previous.ModerationState.Normalized() != next.ModerationState.Normalized() {
		return contracts.EventTypeUpdated, true
	}
	return "", false
}
