package contracts

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	EventTypeStatusChanged = "deliveries.delivery.status_changed"
	EventVersion           = 1
)

var ErrInvalidEvent = errors.New("invalid deliveries event")

// StatusChangedPayload is a logistics-state change. Addresses and location are omitted.
type StatusChangedPayload struct {
	DeliveryID      string `json:"delivery_id"`
	TransactionID   string `json:"transaction_id"`
	RequesterUserID string `json:"requester_user_id"`
	ProviderUserID  string `json:"provider_user_id"`
	ActorUserID     string `json:"actor_user_id"`
	StatusCode      string `json:"status_code"`
}

func DecodeStatusChanged(raw json.RawMessage) (StatusChangedPayload, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return StatusChangedPayload{}, ErrInvalidEvent
	}
	var p StatusChangedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return StatusChangedPayload{}, ErrInvalidEvent
	}
	p.DeliveryID = strings.TrimSpace(p.DeliveryID)
	p.TransactionID = strings.TrimSpace(p.TransactionID)
	p.RequesterUserID = strings.TrimSpace(p.RequesterUserID)
	p.ProviderUserID = strings.TrimSpace(p.ProviderUserID)
	p.ActorUserID = strings.TrimSpace(p.ActorUserID)
	p.StatusCode = strings.TrimSpace(p.StatusCode)
	if p.DeliveryID == "" || p.TransactionID == "" || p.RequesterUserID == "" ||
		p.ProviderUserID == "" || p.ActorUserID == "" || p.StatusCode == "" {
		return StatusChangedPayload{}, ErrInvalidEvent
	}
	return p, nil
}
