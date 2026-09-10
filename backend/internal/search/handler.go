package search

import (
	"context"
	"encoding/json"

	listingcontracts "backend/internal/listings/contracts"
	locationcontracts "backend/internal/location/contracts"
	"backend/internal/platform/outbox"
)

// ProjectionHandler consumes listing and location outbox events and rebuilds
// the derived search projection from owning-domain contracts.
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
	if !knownSearchEvent(event.EventType, event.EventVersion) {
		return errInvalidEvent
	}
	listingID, err := listingIDFromEvent(event)
	if err != nil {
		return err
	}
	if err := h.projector.Rebuild(ctx, listingID); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func knownSearchEvent(eventType string, version int) bool {
	if version != listingcontracts.EventVersion && version != locationcontracts.EventVersion {
		return false
	}
	switch eventType {
	case listingcontracts.EventTypePublished,
		listingcontracts.EventTypeUpdated,
		listingcontracts.EventTypeArchived,
		locationcontracts.EventTypeListingChanged:
		return version == 1
	default:
		return false
	}
}

func listingIDFromEvent(event outbox.Event) (ID, error) {
	raw := event.Payload
	if len(raw) == 0 || !json.Valid(raw) {
		return ID{}, errInvalidEvent
	}
	idStr, err := listingcontracts.DecodeListingIDPayload(raw)
	if err != nil {
		idStr, err = locationcontracts.DecodeListingChangedPayload(raw)
		if err != nil {
			return ID{}, errInvalidEvent
		}
	}
	id, err := ParseID(idStr)
	if err != nil {
		return ID{}, errInvalidEvent
	}
	return id, nil
}

var _ outbox.Handler = (*ProjectionHandler)(nil)
