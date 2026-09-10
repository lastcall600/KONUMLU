package verified

import (
	"encoding/json"
	"fmt"
	"time"

	"backend/internal/platform/outbox"
	verifiedcontracts "backend/internal/verified/contracts"
)

const interactionAggregateType = "verified_interaction"

func encodeInteractionCompleted(v VerifiedInteraction) (outbox.NewEvent, error) {
	payload := verifiedcontracts.InteractionCompletedPayload{
		InteractionID:      v.ID.String(),
		ListingID:          v.ListingID.String(),
		RequesterUserID:    v.RequesterUserID.String(),
		ProviderUserID:     v.ProviderUserID.String(),
		InteractionType:    v.InteractionType,
		VerificationMethod: v.VerificationMethod,
		VerifiedAt:         v.VerifiedAt.UTC().Format(time.RFC3339),
	}
	scopeID := v.AppointmentID
	if !v.AppointmentID.IsZero() {
		payload.AppointmentID = v.AppointmentID.String()
	}
	if !v.FlowID.IsZero() {
		payload.FlowID = v.FlowID.String()
		scopeID = v.FlowID
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return outbox.NewEvent{}, errUnavailable
	}
	if scopeID.IsZero() {
		return outbox.NewEvent{}, errInvalidInteraction
	}
	return outbox.NewEvent{
		EventType:      EventTypeInteractionCompleted,
		EventVersion:   EventVersion,
		AggregateType:  interactionAggregateType,
		AggregateID:    v.ID.String(),
		Payload:        raw,
		IdempotencyKey: fmt.Sprintf("%s:%s", EventTypeInteractionCompleted, scopeID.String()),
	}, nil
}
