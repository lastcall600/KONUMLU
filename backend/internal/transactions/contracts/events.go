package contracts

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	EventTypeCreated   = "transactions.transaction.created"
	EventTypeCompleted = "transactions.transaction.completed"
	EventTypeCancelled = "transactions.transaction.cancelled"
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
	ProviderUserID  string `json:"provider_user_id,omitempty"`
	ActorUserID     string `json:"actor_user_id,omitempty"`
	CompletedAt     string `json:"completed_at"`
}

// LifecyclePayload is created/cancelled Transaction truth. Amounts and Need text are omitted.
type LifecyclePayload struct {
	TransactionID   string `json:"transaction_id"`
	OfferID         string `json:"offer_id"`
	NeedID          string `json:"need_id"`
	RequesterUserID string `json:"requester_user_id"`
	ProviderUserID  string `json:"provider_user_id"`
	ActorUserID     string `json:"actor_user_id"`
	StatusCode      string `json:"status_code"`
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
	p.ProviderUserID = strings.TrimSpace(p.ProviderUserID)
	p.ActorUserID = strings.TrimSpace(p.ActorUserID)
	if p.TransactionID == "" || p.OfferID == "" || p.NeedID == "" ||
		p.RequesterUserID == "" || p.CompletedAt == "" {
		return CompletedPayload{}, ErrInvalidEvent
	}
	return p, nil
}

func DecodeLifecycle(raw json.RawMessage) (LifecyclePayload, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return LifecyclePayload{}, ErrInvalidEvent
	}
	var p LifecyclePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return LifecyclePayload{}, ErrInvalidEvent
	}
	p.TransactionID = strings.TrimSpace(p.TransactionID)
	p.OfferID = strings.TrimSpace(p.OfferID)
	p.NeedID = strings.TrimSpace(p.NeedID)
	p.RequesterUserID = strings.TrimSpace(p.RequesterUserID)
	p.ProviderUserID = strings.TrimSpace(p.ProviderUserID)
	p.ActorUserID = strings.TrimSpace(p.ActorUserID)
	p.StatusCode = strings.TrimSpace(p.StatusCode)
	if p.TransactionID == "" || p.OfferID == "" || p.NeedID == "" ||
		p.RequesterUserID == "" || p.ProviderUserID == "" || p.StatusCode == "" {
		return LifecyclePayload{}, ErrInvalidEvent
	}
	return p, nil
}
