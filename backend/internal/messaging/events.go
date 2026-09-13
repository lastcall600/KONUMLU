package messaging

import (
	"encoding/json"
	"fmt"

	msgcontracts "backend/internal/messaging/contracts"
	"backend/internal/platform/outbox"
)

const messageAggregateType = "message"

func encodeMessageReceived(msg Message, recipient ID) (outbox.NewEvent, error) {
	if msg.ID.IsZero() || recipient.IsZero() || msg.SenderUserID == recipient {
		return outbox.NewEvent{}, errUnavailable
	}
	payload, err := json.Marshal(msgcontracts.ReceivedPayload{
		MessageID:       msg.ID.String(),
		ConversationID:  msg.ConversationID.String(),
		SenderUserID:    msg.SenderUserID.String(),
		RecipientUserID: recipient.String(),
	})
	if err != nil {
		return outbox.NewEvent{}, errUnavailable
	}
	return outbox.NewEvent{
		EventType:      msgcontracts.EventTypeMessageReceived,
		EventVersion:   msgcontracts.EventVersion,
		AggregateType:  messageAggregateType,
		AggregateID:    msg.ID.String(),
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("%s:%s:%s", msgcontracts.EventTypeMessageReceived, msg.ID.String(), recipient.String()),
	}, nil
}
