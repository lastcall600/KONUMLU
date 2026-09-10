package contracts

import (
	"encoding/json"
	"strings"
)

const (
	EventTypeListingChanged = "location.listing.changed"
	EventVersion            = 1
)

// ListingChangedPayload is the V1 outbox payload for listing geo changes.
type ListingChangedPayload struct {
	ListingID string `json:"listing_id"`
}

func DecodeListingChangedPayload(raw json.RawMessage) (string, error) {
	var p ListingChangedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", ErrInvalidEvent
	}
	id := strings.TrimSpace(p.ListingID)
	if id == "" {
		return "", ErrInvalidEvent
	}
	return id, nil
}
