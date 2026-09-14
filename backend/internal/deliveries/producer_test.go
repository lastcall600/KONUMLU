package deliveries

import (
	"context"
	"strings"
	"testing"

	delcontracts "backend/internal/deliveries/contracts"
	"backend/internal/platform/outbox"
)

type recordingOutbox struct {
	events []outbox.NewEvent
}

func (r *recordingOutbox) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	r.events = append(r.events, in)
	return outbox.Event{}, nil
}

func TestDeliveryStatusChangeEnqueuesSafePayload(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	rec := &recordingOutbox{}
	svc.SetOutbox(nil, rec)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 1 || rec.events[0].EventType != delcontracts.EventTypeStatusChanged {
		t.Fatalf("create events=%+v", rec.events)
	}
	p, err := delcontracts.DecodeStatusChanged(rec.events[0].Payload)
	if err != nil || p.StatusCode != string(StatusPending) || p.RequesterUserID != fx.requester.String() {
		t.Fatalf("payload=%+v err=%v", p, err)
	}
	if strings.Contains(strings.ToLower(string(rec.events[0].Payload)), "address") ||
		strings.Contains(strings.ToLower(string(rec.events[0].Payload)), "lat") {
		t.Fatal("location leaked")
	}
	if _, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{}); err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("idempotent create events=%d", len(rec.events))
	}
	if _, err := svc.MarkReady(context.Background(), fx.provider, d.ID); err != nil {
		t.Fatal(err)
	}
	if len(rec.events) != 2 {
		t.Fatalf("ready events=%d", len(rec.events))
	}
	p2, err := delcontracts.DecodeStatusChanged(rec.events[1].Payload)
	if err != nil || p2.StatusCode != string(StatusReady) {
		t.Fatalf("ready payload=%+v err=%v", p2, err)
	}
}
