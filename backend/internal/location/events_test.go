package location

import (
	"context"
	"encoding/json"
	"testing"

	"backend/internal/location/contracts"
	"backend/internal/platform/outbox"
)

func TestSetListingLocationEnqueuesChangedEvent(t *testing.T) {
	svc, _, _ := mustService(t)
	enq := &memoryEnqueuer{}
	svc.SetOutbox(enq)
	listingID := mustID(t)
	got, err := svc.SetListingLocation(context.Background(), mustWriteScope(t, listingID), Coordinates{Latitude: 36.621, Longitude: 29.116}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != 1 || enq.events[0].EventType != contracts.EventTypeListingChanged {
		t.Fatalf("events = %+v", enq.events)
	}
	var payload contracts.ListingChangedPayload
	if err := json.Unmarshal(enq.events[0].Payload, &payload); err != nil || payload.ListingID != got.ListingID.String() {
		t.Fatalf("payload = %+v err = %v", payload, err)
	}
}

type memoryEnqueuer struct {
	events []outbox.NewEvent
}

func (e *memoryEnqueuer) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	e.events = append(e.events, in)
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	return outbox.Event{ID: id, EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
}
