package contracts

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	EventTypeVerifiedCreated = "reviews.verified.created"
	EventVersion             = 1
)

var ErrInvalidEvent = errors.New("invalid reviews event")

// VerifiedCreatedPayload is the V1 outbox contract for a verified-interaction review.
// Trust and other consumers must import this package, not reviews implementation types.
// Body is intentionally omitted.
type VerifiedCreatedPayload struct {
	ReviewID              string `json:"review_id"`
	VerifiedInteractionID string `json:"verified_interaction_id"`
	ListingID             string `json:"listing_id"`
	ReviewerUserID        string `json:"reviewer_user_id"`
	ProviderUserID        string `json:"provider_user_id"`
	ListingAccuracy       int    `json:"listing_accuracy"`
	ProviderService       int    `json:"provider_service"`
	CreatedAt             string `json:"created_at,omitempty"`
}

func DecodeVerifiedCreated(raw json.RawMessage) (VerifiedCreatedPayload, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return VerifiedCreatedPayload{}, ErrInvalidEvent
	}
	var p VerifiedCreatedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return VerifiedCreatedPayload{}, ErrInvalidEvent
	}
	p.ReviewID = strings.TrimSpace(p.ReviewID)
	p.VerifiedInteractionID = strings.TrimSpace(p.VerifiedInteractionID)
	p.ListingID = strings.TrimSpace(p.ListingID)
	p.ReviewerUserID = strings.TrimSpace(p.ReviewerUserID)
	p.ProviderUserID = strings.TrimSpace(p.ProviderUserID)
	p.CreatedAt = strings.TrimSpace(p.CreatedAt)
	if p.ReviewID == "" || p.VerifiedInteractionID == "" || p.ListingID == "" ||
		p.ReviewerUserID == "" || p.ProviderUserID == "" || p.CreatedAt == "" {
		return VerifiedCreatedPayload{}, ErrInvalidEvent
	}
	if p.ReviewerUserID == p.ProviderUserID {
		return VerifiedCreatedPayload{}, ErrInvalidEvent
	}
	if p.ListingAccuracy < 1 || p.ListingAccuracy > 5 || p.ProviderService < 1 || p.ProviderService > 5 {
		return VerifiedCreatedPayload{}, ErrInvalidEvent
	}
	return p, nil
}
