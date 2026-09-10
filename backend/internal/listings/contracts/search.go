package contracts

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const (
	EventTypePublished = "listings.listing.published"
	EventTypeUpdated   = "listings.listing.updated"
	EventTypeArchived  = "listings.listing.archived"
	EventVersion       = 1

	StatusPublished = "published"
	StatusArchived  = "archived"
)

// ListingIDPayload is the V1 outbox payload for listing search events.
type ListingIDPayload struct {
	ListingID string `json:"listing_id"`
}

// ListingSnapshot is the searchable listing surface. It is not a table handle.
type ListingSnapshot struct {
	ID                    ID
	Status                string
	ModerationState       string
	CategoryID            ID
	CategorySchemaVersion int64
	Title                 string
	Description           string
	PriceAmount           *string
	PriceCurrency         *string
	Attributes            map[string]any
	PublishedAt           *time.Time
	UpdatedAt             time.Time
}

// SearchSource is the Listings read Search may call to rebuild a projection.
type SearchSource interface {
	GetListingSnapshot(ctx context.Context, listingID ID) (ListingSnapshot, error)
}

func DecodeListingIDPayload(raw json.RawMessage) (string, error) {
	var p ListingIDPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", ErrInvalidEvent
	}
	id := strings.TrimSpace(p.ListingID)
	if id == "" {
		return "", ErrInvalidEvent
	}
	return id, nil
}
