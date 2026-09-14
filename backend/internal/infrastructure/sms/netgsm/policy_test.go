package netgsm

import (
	"context"
	"net/http/httptest"
	"testing"

	domain "backend/internal/notifications"
	"backend/internal/notifications/policy"
)

func TestChannelClientIsTransportOnly(t *testing.T) {
	cap := &captured{}
	srv := httptest.NewServer(cap.handler())
	t.Cleanup(srv.Close)
	ch, err := NewChannelClient(mustTransport(t, srv, 0))
	if err != nil {
		t.Fatal(err)
	}
	// Consent, preference, and account-state are dispatcher concerns.
	_, err = ch.Send(context.Background(), domain.ChannelSendRequest{
		Channel:        policy.ChannelSMS,
		Destination:    "+905551112233",
		TemplateKey:    "offer.received",
		Locale:         "tr",
		IdempotencyKey: "delivery-policy-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.n.Load() != 1 {
		t.Fatalf("calls=%d", cap.n.Load())
	}
}
