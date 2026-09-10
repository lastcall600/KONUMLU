package location

import (
	"encoding/json"
	"fmt"
	"time"

	"backend/internal/location/contracts"
	"backend/internal/platform/outbox"
)

const listingLocationAggregateType = "listing_location"

func encodeListingChangedEvent(listingID ID, at time.Time) (outbox.NewEvent, error) {
	payload, err := json.Marshal(contracts.ListingChangedPayload{ListingID: listingID.String()})
	if err != nil {
		return outbox.NewEvent{}, errUnavailable
	}
	return outbox.NewEvent{
		EventType:      contracts.EventTypeListingChanged,
		EventVersion:   contracts.EventVersion,
		AggregateType:  listingLocationAggregateType,
		AggregateID:    listingID.String(),
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", contracts.EventTypeListingChanged, listingID.String(), at.UTC().UnixNano()),
	}, nil
}
