package media

import (
	"encoding/json"
	"strings"

	"backend/internal/platform/outbox"
)

const (
	ProcessEventType      = "media.image.process"
	ProcessEventVersion   = 1
	processAggregateType  = "media_asset"
	processIdempotencyPref = "media.image.process:"
)

// ProcessPayload is the V1 outbox payload. It contains only the asset reference.
type ProcessPayload struct {
	AssetID string `json:"asset_id"`
}

func encodeProcessEvent(assetID ID) (outbox.NewEvent, error) {
	payload, err := json.Marshal(ProcessPayload{AssetID: assetID.String()})
	if err != nil {
		return outbox.NewEvent{}, errUnavailable
	}
	return outbox.NewEvent{
		EventType:      ProcessEventType,
		EventVersion:   ProcessEventVersion,
		AggregateType:  processAggregateType,
		AggregateID:    assetID.String(),
		Payload:        payload,
		IdempotencyKey: processIdempotencyPref + assetID.String(),
	}, nil
}

func decodeProcessPayload(raw json.RawMessage) (ID, error) {
	var p ProcessPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return ID{}, errInvalidAsset
	}
	id, err := ParseID(strings.TrimSpace(p.AssetID))
	if err != nil {
		return ID{}, errInvalidAsset
	}
	return id, nil
}
