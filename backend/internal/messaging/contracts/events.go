package contracts

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	EventTypeMessageReceived = "messaging.message.received"
	EventVersion             = 1
)

var ErrInvalidEvent = errors.New("invalid messaging event")

// ReceivedPayload is the V1 outbox contract after a message is persisted.
// Body text is omitted. Recipients are conversation participants excluding the sender.
type ReceivedPayload struct {
	MessageID      string `json:"message_id"`
	ConversationID string `json:"conversation_id"`
	SenderUserID   string `json:"sender_user_id"`
	RecipientUserID string `json:"recipient_user_id"`
}

func DecodeReceived(raw json.RawMessage) (ReceivedPayload, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return ReceivedPayload{}, ErrInvalidEvent
	}
	var p ReceivedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return ReceivedPayload{}, ErrInvalidEvent
	}
	p.MessageID = strings.TrimSpace(p.MessageID)
	p.ConversationID = strings.TrimSpace(p.ConversationID)
	p.SenderUserID = strings.TrimSpace(p.SenderUserID)
	p.RecipientUserID = strings.TrimSpace(p.RecipientUserID)
	if p.MessageID == "" || p.ConversationID == "" || p.SenderUserID == "" || p.RecipientUserID == "" {
		return ReceivedPayload{}, ErrInvalidEvent
	}
	if p.SenderUserID == p.RecipientUserID {
		return ReceivedPayload{}, ErrInvalidEvent
	}
	return p, nil
}
