package offers

import (
	"context"
	"strings"
	"testing"

	offercontracts "backend/internal/offers/contracts"
	"backend/internal/platform/outbox"
)

type recordingOutbox struct {
	events []outbox.NewEvent
}

func (r *recordingOutbox) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	r.events = append(r.events, in)
	return outbox.Event{}, nil
}

func TestOfferLifecycleEnqueuesControlledEvents(t *testing.T) {
	svc, fx := mustOfferService(t)
	rec := &recordingOutbox{}
	svc.SetOutbox(nil, rec)
	offer, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID, Message: "secret-text",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 1 || rec.events[0].EventType != offercontracts.EventTypeSubmitted {
		t.Fatalf("submit events=%+v", rec.events)
	}
	p, err := offercontracts.DecodeTransition(rec.events[0].Payload)
	if err != nil || p.RequesterUserID != fx.requester.String() || p.ProviderUserID != fx.provider.String() {
		t.Fatalf("payload=%+v err=%v", p, err)
	}
	if strings.Contains(string(rec.events[0].Payload), "secret-text") {
		t.Fatal("offer message leaked")
	}
	again, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	})
	if err != nil || again.ID != offer.ID || len(rec.events) != 1 {
		t.Fatalf("idempotent create events=%d err=%v", len(rec.events), err)
	}
	if _, err := svc.Accept(context.Background(), fx.requester, fx.needID, offer.ID); err != nil {
		t.Fatal(err)
	}
	if countType(rec.events, offercontracts.EventTypeAccepted) != 1 {
		t.Fatalf("accept events=%+v", rec.events)
	}
	if _, err := svc.Accept(context.Background(), fx.requester, fx.needID, offer.ID); err != nil {
		t.Fatal(err)
	}
	if countType(rec.events, offercontracts.EventTypeAccepted) != 1 {
		t.Fatalf("replay accept events=%+v", rec.events)
	}
}

func TestAcceptEnqueuesRejectedSiblings(t *testing.T) {
	svc, fx := mustOfferService(t)
	rec := &recordingOutbox{}
	svc.SetOutbox(nil, rec)
	first, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	fx.serviceID = mustID(t)
	second, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(context.Background(), fx.requester, fx.needID, first.ID); err != nil {
		t.Fatal(err)
	}
	if countType(rec.events, offercontracts.EventTypeAccepted) != 1 {
		t.Fatalf("accepted=%+v", rec.events)
	}
	if countType(rec.events, offercontracts.EventTypeRejected) != 1 {
		t.Fatalf("rejected sibling missing: %+v second=%s", rec.events, second.ID)
	}
}

func TestWithdrawDoesNotEnqueue(t *testing.T) {
	svc, fx := mustOfferService(t)
	rec := &recordingOutbox{}
	svc.SetOutbox(nil, rec)
	offer, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Withdraw(context.Background(), fx.provider, offer.ID); err != nil {
		t.Fatal(err)
	}
	if countType(rec.events, offercontracts.EventTypeSubmitted) != 1 || len(rec.events) != 1 {
		t.Fatalf("withdraw must not notify: %+v", rec.events)
	}
}

func countType(events []outbox.NewEvent, typ string) int {
	n := 0
	for _, e := range events {
		if e.EventType == typ {
			n++
		}
	}
	return n
}
