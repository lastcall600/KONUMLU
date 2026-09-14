package disputes

import (
	"encoding/json"
	"fmt"

	dispcontracts "backend/internal/disputes/contracts"
	"backend/internal/platform/outbox"
)

const disputeAggregateType = "dispute"

func encodeUpdated(d Dispute, actorUserID ID) (outbox.NewEvent, error) {
	if d.ID.IsZero() {
		return outbox.NewEvent{}, errUnavailable
	}
	actor := ""
	if !actorUserID.IsZero() {
		actor = actorUserID.String()
	}
	payload, err := json.Marshal(dispcontracts.UpdatedPayload{
		DisputeID:       d.ID.String(),
		TransactionID:   d.TransactionID.String(),
		RequesterUserID: d.RequesterUserID.String(),
		ProviderUserID:  d.ProviderUserID.String(),
		ActorUserID:     actor,
		StatusCode:      string(d.Status),
	})
	if err != nil {
		return outbox.NewEvent{}, errUnavailable
	}
	return outbox.NewEvent{
		EventType:      dispcontracts.EventTypeUpdated,
		EventVersion:   dispcontracts.EventVersion,
		AggregateType:  disputeAggregateType,
		AggregateID:    d.ID.String(),
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", dispcontracts.EventTypeUpdated, d.ID.String(), string(d.Status), actor),
	}, nil
}
