package contracts

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	EventTypeUpdated = "disputes.dispute.updated"
	EventVersion     = 1
)

var ErrInvalidEvent = errors.New("invalid disputes event")

// UpdatedPayload is a generic participant-safe dispute update. Evidence and notes are omitted.
type UpdatedPayload struct {
	DisputeID       string `json:"dispute_id"`
	TransactionID   string `json:"transaction_id"`
	RequesterUserID string `json:"requester_user_id"`
	ProviderUserID  string `json:"provider_user_id"`
	ActorUserID     string `json:"actor_user_id"`
	StatusCode      string `json:"status_code"`
}

func DecodeUpdated(raw json.RawMessage) (UpdatedPayload, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return UpdatedPayload{}, ErrInvalidEvent
	}
	var p UpdatedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return UpdatedPayload{}, ErrInvalidEvent
	}
	p.DisputeID = strings.TrimSpace(p.DisputeID)
	p.TransactionID = strings.TrimSpace(p.TransactionID)
	p.RequesterUserID = strings.TrimSpace(p.RequesterUserID)
	p.ProviderUserID = strings.TrimSpace(p.ProviderUserID)
	p.ActorUserID = strings.TrimSpace(p.ActorUserID)
	p.StatusCode = strings.TrimSpace(p.StatusCode)
	if p.DisputeID == "" || p.TransactionID == "" || p.RequesterUserID == "" ||
		p.ProviderUserID == "" || p.StatusCode == "" {
		return UpdatedPayload{}, ErrInvalidEvent
	}
	return p, nil
}
