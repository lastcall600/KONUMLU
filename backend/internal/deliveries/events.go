package deliveries

import (
	"encoding/json"
	"fmt"

	delcontracts "backend/internal/deliveries/contracts"
	"backend/internal/platform/outbox"
)

const deliveryAggregateType = "delivery"

func encodeStatusChanged(d Delivery, actorUserID ID) (outbox.NewEvent, error) {
	if d.ID.IsZero() || actorUserID.IsZero() {
		return outbox.NewEvent{}, errUnavailable
	}
	payload, err := json.Marshal(delcontracts.StatusChangedPayload{
		DeliveryID:      d.ID.String(),
		TransactionID:   d.TransactionID.String(),
		RequesterUserID: d.RequesterUserID.String(),
		ProviderUserID:  d.ProviderUserID.String(),
		ActorUserID:     actorUserID.String(),
		StatusCode:      string(d.Status),
	})
	if err != nil {
		return outbox.NewEvent{}, errUnavailable
	}
	return outbox.NewEvent{
		EventType:      delcontracts.EventTypeStatusChanged,
		EventVersion:   delcontracts.EventVersion,
		AggregateType:  deliveryAggregateType,
		AggregateID:    d.ID.String(),
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("%s:%s:%s", delcontracts.EventTypeStatusChanged, d.ID.String(), string(d.Status)),
	}, nil
}
