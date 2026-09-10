package contracts

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	EventTypeCompleted = "transactions.transaction.completed"
	EventVersion       = 1
)

var ErrInvalidEvent = errors.New("invalid transactions event")

// CompletedPayload is the V1 outbox contract for a completed Transaction.
// Need fulfillment consumers must import this package, not transactions implementation types.
// RequesterUserID is the Transaction's stored requester, never a client-supplied identity.
type CompletedPayload struct {
	TransactionID   string `json:"transaction_id"`
	OfferID         string `json:"offer_id"`
	NeedID          string `json:"need_id"`
	RequesterUserID string `json:"requester_user_id"`
	CompletedAt     string `json:"completed_at"`
}

func DecodeCompleted(raw json.RawMessage) (CompletedPayload, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return CompletedPayload{}, ErrInvalidEvent
	}
	var p CompletedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return CompletedPayload{}, ErrInvalidEvent
	}
	p.TransactionID = strings.TrimSpace(p.TransactionID)
	p.OfferID = strings.TrimSpace(p.OfferID)
	p.NeedID = strings.TrimSpace(p.NeedID)
	p.RequesterUserID = strings.TrimSpace(p.RequesterUserID)
	p.CompletedAt = strings.TrimSpace(p.CompletedAt)
	if p.TransactionID == "" || p.OfferID == "" || p.NeedID == "" ||
		p.RequesterUserID == "" || p.CompletedAt == "" {
		return CompletedPayload{}, ErrInvalidEvent
	}
	return p, nil
}
