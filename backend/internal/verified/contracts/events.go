package contracts

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	EventTypeInteractionCompleted = "verified.interaction.completed"
	EventVersion                  = 1
)

var ErrInvalidEvent = errors.New("invalid verified event")

// InteractionCompletedPayload is the V1 outbox contract for a completed
// verified interaction. Trust and other consumers must import this package,
// not verified implementation types.
type InteractionCompletedPayload struct {
	InteractionID      string `json:"interaction_id"`
	AppointmentID      string `json:"appointment_id,omitempty"`
	FlowID             string `json:"flow_id,omitempty"`
	ListingID          string `json:"listing_id"`
	RequesterUserID    string `json:"requester_user_id"`
	ProviderUserID     string `json:"provider_user_id"`
	InteractionType    string `json:"interaction_type"`
	VerificationMethod string `json:"verification_method"`
	VerifiedAt         string `json:"verified_at,omitempty"`
}

func DecodeInteractionCompleted(raw json.RawMessage) (InteractionCompletedPayload, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return InteractionCompletedPayload{}, ErrInvalidEvent
	}
	var p InteractionCompletedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return InteractionCompletedPayload{}, ErrInvalidEvent
	}
	p.InteractionID = strings.TrimSpace(p.InteractionID)
	p.AppointmentID = strings.TrimSpace(p.AppointmentID)
	p.FlowID = strings.TrimSpace(p.FlowID)
	p.ListingID = strings.TrimSpace(p.ListingID)
	p.RequesterUserID = strings.TrimSpace(p.RequesterUserID)
	p.ProviderUserID = strings.TrimSpace(p.ProviderUserID)
	p.InteractionType = strings.TrimSpace(p.InteractionType)
	p.VerificationMethod = strings.TrimSpace(p.VerificationMethod)
	p.VerifiedAt = strings.TrimSpace(p.VerifiedAt)
	if p.InteractionID == "" || p.ListingID == "" ||
		p.RequesterUserID == "" || p.ProviderUserID == "" ||
		p.InteractionType == "" || p.VerificationMethod == "" {
		return InteractionCompletedPayload{}, ErrInvalidEvent
	}
	if p.RequesterUserID == p.ProviderUserID {
		return InteractionCompletedPayload{}, ErrInvalidEvent
	}
	switch p.InteractionType {
	case InteractionTypeListingInspection:
		if p.AppointmentID == "" || p.FlowID != "" {
			return InteractionCompletedPayload{}, ErrInvalidEvent
		}
	case InteractionTypeTransaction, InteractionTypeDelivery:
		if p.FlowID == "" || p.AppointmentID != "" {
			return InteractionCompletedPayload{}, ErrInvalidEvent
		}
	default:
		return InteractionCompletedPayload{}, ErrInvalidEvent
	}
	return p, nil
}
