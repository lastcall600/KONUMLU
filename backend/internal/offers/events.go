package offers

import (
	"encoding/json"
	"fmt"

	offercontracts "backend/internal/offers/contracts"
	"backend/internal/platform/outbox"
)

const offerAggregateType = "offer"

func encodeOfferTransition(eventType, transition string, offer Offer, requesterUserID, actorUserID ID) (outbox.NewEvent, error) {
	if offer.ID.IsZero() || requesterUserID.IsZero() || actorUserID.IsZero() {
		return outbox.NewEvent{}, errUnavailable
	}
	payload, err := json.Marshal(offercontracts.TransitionPayload{
		OfferID:         offer.ID.String(),
		NeedID:          offer.NeedID.String(),
		ProviderUserID:  offer.ProviderUserID.String(),
		RequesterUserID: requesterUserID.String(),
		ActorUserID:     actorUserID.String(),
		Transition:      transition,
	})
	if err != nil {
		return outbox.NewEvent{}, errUnavailable
	}
	return outbox.NewEvent{
		EventType:      eventType,
		EventVersion:   offercontracts.EventVersion,
		AggregateType:  offerAggregateType,
		AggregateID:    offer.ID.String(),
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("%s:%s:%s", eventType, offer.ID.String(), transition),
	}, nil
}
