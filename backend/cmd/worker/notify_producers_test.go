package main

import (
	"context"
	"testing"

	"backend/internal/notifications"
	"backend/internal/notifications/policy"
	offercontracts "backend/internal/offers/contracts"
	"backend/internal/platform/outbox"
)

func TestMarketplaceHandlerRejectsUnknownEvent(t *testing.T) {
	h := newMarketplaceNotifyHandler(nil)
	err := h.Handle(context.Background(), outbox.Event{EventType: "client.invented", EventVersion: 1, Payload: []byte(`{}`)})
	if err != notifications.ErrInvalidEvent {
		t.Fatalf("err=%v", err)
	}
}

func TestHandleOfferInvalidPayloadIsDropped(t *testing.T) {
	err := handleOffer(context.Background(), nil, outbox.Event{Payload: []byte(`{`)}, policy.EventOfferReceived, true)
	if err != nil {
		t.Fatalf("invalid payload must not fail the outbox row: %v", err)
	}
}

func TestOfferSubmittedEventNameIsServerOwned(t *testing.T) {
	if offercontracts.EventTypeSubmitted != "offers.offer.submitted" {
		t.Fatalf("event type changed: %s", offercontracts.EventTypeSubmitted)
	}
}
