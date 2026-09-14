package ses

import (
	"context"
	"testing"

	domain "backend/internal/notifications"
	"backend/internal/notifications/policy"
)

func TestChannelClientIsTransportOnly(t *testing.T) {
	api := &fakeSES{}
	ch, err := NewChannelClient(mustTransport(t, api))
	if err != nil {
		t.Fatal(err)
	}
	// Consent, preference, and account-state are dispatcher concerns.
	// A direct ChannelSender call always hits SES for a valid email request.
	_, err = ch.Send(context.Background(), domain.ChannelSendRequest{
		Channel:        policy.ChannelEmail,
		Destination:    "owner@example.test",
		TemplateKey:    "offer.received",
		Locale:         "tr",
		IdempotencyKey: "delivery-policy-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if api.n != 1 {
		t.Fatalf("calls=%d", api.n)
	}
}
