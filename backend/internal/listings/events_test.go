package listings

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"backend/internal/listings/contracts"
	"backend/internal/platform/outbox"
)

func TestPublishAndArchiveEnqueueSearchEvents(t *testing.T) {
	svc, _, now := mustService(t)
	enq := &memoryEnqueuer{}
	svc.SetOutbox(nil, enq)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	ready, err := svc.MarkReady(context.Background(), created.ID, created.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != 0 {
		t.Fatalf("ready must not emit search events: %+v", enq.events)
	}
	now.now = now.now.Add(time.Minute)
	published, err := svc.Publish(context.Background(), ready.ID, ready.UpdatedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != 1 || enq.events[0].EventType != contracts.EventTypePublished {
		t.Fatalf("publish events = %+v", enq.events)
	}
	var payload contracts.ListingIDPayload
	if err := json.Unmarshal(enq.events[0].Payload, &payload); err != nil || payload.ListingID != published.ID.String() {
		t.Fatalf("payload = %+v err = %v", payload, err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Archive(context.Background(), published.ID, published.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != 2 || enq.events[1].EventType != contracts.EventTypeArchived {
		t.Fatalf("archive events = %+v", enq.events)
	}
}

func TestRestrictEnqueueSearchUpdatedEvent(t *testing.T) {
	svc, _, now := mustService(t)
	enq := &memoryEnqueuer{}
	svc.SetOutbox(nil, enq)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	ready, err := svc.MarkReady(context.Background(), created.ID, created.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	published, err := svc.Publish(context.Background(), ready.ID, ready.UpdatedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyModerationInput{
		ListingID: contracts.ID(published.ID), State: contracts.ModerationStateRemoved,
	}); err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != 2 || enq.events[1].EventType != contracts.EventTypeUpdated {
		t.Fatalf("restrict events = %+v", enq.events)
	}
	now.now = now.now.Add(time.Minute)
	if err := svc.ClearModerationState(context.Background(), contracts.ID(published.ID)); err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != 3 || enq.events[2].EventType != contracts.EventTypeUpdated {
		t.Fatalf("restore events = %+v", enq.events)
	}
}

func TestClearModerationOnArchivedEnqueuesSearchUpdated(t *testing.T) {
	svc, _, now := mustService(t)
	enq := &memoryEnqueuer{}
	svc.SetOutbox(nil, enq)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	ready, err := svc.MarkReady(context.Background(), created.ID, created.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	published, err := svc.Publish(context.Background(), ready.ID, ready.UpdatedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if err := svc.ApplyModerationState(context.Background(), contracts.ApplyModerationInput{
		ListingID: contracts.ID(published.ID), State: contracts.ModerationStateRemoved,
	}); err != nil {
		t.Fatal(err)
	}
	hidden, err := svc.Get(context.Background(), published.ID)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	archived, err := svc.Archive(context.Background(), hidden.ID, hidden.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	before := len(enq.events)
	now.now = now.now.Add(time.Minute)
	if err := svc.ClearModerationState(context.Background(), contracts.ID(archived.ID)); err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != before+1 || enq.events[len(enq.events)-1].EventType != contracts.EventTypeUpdated {
		t.Fatalf("archived restore events = %+v", enq.events)
	}
}

func TestGetListingSnapshot(t *testing.T) {
	svc, _, _ := mustService(t)
	created, err := svc.CreateDraft(context.Background(), mustID(t), validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := svc.GetListingSnapshot(context.Background(), contracts.ID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if snap.ID != contracts.ID(created.ID) || snap.Status != string(StatusDraft) || snap.Title != "Bike" {
		t.Fatalf("snap = %+v", snap)
	}
	if snap.ModerationState != contracts.ModerationStateNone || snap.PubliclyVisible() {
		t.Fatalf("draft snap visibility = %+v", snap)
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
