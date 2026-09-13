package messaging

import (
	"context"
	"strings"
	"testing"

	msgcontracts "backend/internal/messaging/contracts"
	"backend/internal/platform/outbox"
)

type recordingOutbox struct {
	events []outbox.NewEvent
}

func (r *recordingOutbox) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	r.events = append(r.events, in)
	return outbox.Event{}, nil
}

func TestSendMessageEnqueuesCounterpartNotSender(t *testing.T) {
	svc, _, listings, _ := newTestService(t)
	rec := &recordingOutbox{}
	svc.SetOutbox(nil, rec)
	buyer, seller, listing := mustID(t), mustID(t), mustID(t)
	listings.setPublished(listing, seller)
	conv, err := svc.CreateConversation(context.Background(), buyer, listing)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := svc.SendMessage(context.Background(), buyer, conv.ID, "merhaba")
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("events=%d", len(rec.events))
	}
	p, err := msgcontracts.DecodeReceived(rec.events[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if p.SenderUserID != buyer.String() || p.RecipientUserID != seller.String() || p.MessageID != msg.ID.String() {
		t.Fatalf("payload=%+v", p)
	}
	if strings.Contains(string(rec.events[0].Payload), "merhaba") {
		t.Fatal("message body must not be in outbox payload")
	}
	rec.events = nil
	if _, err := svc.SendMessage(context.Background(), seller, conv.ID, "selam"); err != nil {
		t.Fatal(err)
	}
	p, err = msgcontracts.DecodeReceived(rec.events[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if p.RecipientUserID != buyer.String() {
		t.Fatalf("seller send recipient=%s", p.RecipientUserID)
	}
}
