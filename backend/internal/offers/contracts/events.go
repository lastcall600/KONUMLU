package contracts

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	EventTypeSubmitted = "offers.offer.submitted"
	EventTypeAccepted  = "offers.offer.accepted"
	EventTypeRejected  = "offers.offer.rejected"
	EventVersion       = 1
)

var ErrInvalidEvent = errors.New("invalid offers event")

// TransitionPayload is a minimal offer lifecycle event. Offer message text is omitted.
type TransitionPayload struct {
	OfferID         string `json:"offer_id"`
	NeedID          string `json:"need_id"`
	ProviderUserID  string `json:"provider_user_id"`
	RequesterUserID string `json:"requester_user_id"`
	ActorUserID     string `json:"actor_user_id"`
	Transition      string `json:"transition"`
}

func DecodeTransition(raw json.RawMessage) (TransitionPayload, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return TransitionPayload{}, ErrInvalidEvent
	}
	var p TransitionPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return TransitionPayload{}, ErrInvalidEvent
	}
	p.OfferID = strings.TrimSpace(p.OfferID)
	p.NeedID = strings.TrimSpace(p.NeedID)
	p.ProviderUserID = strings.TrimSpace(p.ProviderUserID)
	p.RequesterUserID = strings.TrimSpace(p.RequesterUserID)
	p.ActorUserID = strings.TrimSpace(p.ActorUserID)
	p.Transition = strings.TrimSpace(p.Transition)
	if p.OfferID == "" || p.NeedID == "" || p.ProviderUserID == "" || p.RequesterUserID == "" ||
		p.ActorUserID == "" || p.Transition == "" {
		return TransitionPayload{}, ErrInvalidEvent
	}
	return p, nil
}
