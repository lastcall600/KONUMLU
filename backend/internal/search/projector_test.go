package search

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	listingcontracts "backend/internal/listings/contracts"
	locationcontracts "backend/internal/location/contracts"
	"backend/internal/platform/outbox"
)

func TestPublishedListingProjects(t *testing.T) {
	p, listings, geo := mustProjector(t)
	id := mustID(t)
	listings.put(publishedSnapshot(id, "Bike", nil))
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Bike" || got.Status != StatusPublished {
		t.Fatalf("doc = %+v", got)
	}
	if geo.got != locationcontracts.ID(id) {
		t.Fatal("rebuild must read location through the contract")
	}
}

func TestNonPublishedListingDoesNotProject(t *testing.T) {
	p, listings, _ := mustProjector(t)
	id := mustID(t)
	snap := publishedSnapshot(id, "Draft", nil)
	snap.Status = "draft"
	listings.put(snap)
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), id); !errors.Is(err, errNotFound) {
		t.Fatalf("get err = %v", err)
	}
	draft := publishedSnapshot(id, "Ready", nil)
	draft.Status = "ready"
	if err := p.Upsert(context.Background(), mustDocFromSnap(t, draft)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), id); !errors.Is(err, errNotFound) {
		t.Fatalf("upsert non-published err = %v", err)
	}
}

func TestUpdateRefreshesProjection(t *testing.T) {
	p, listings, _ := mustProjector(t)
	id := mustID(t)
	listings.put(publishedSnapshot(id, "Bike", nil))
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	listings.put(publishedSnapshot(id, "E-Bike", nil))
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get(context.Background(), id)
	if err != nil || got.Title != "E-Bike" {
		t.Fatalf("got = %+v err = %v", got, err)
	}
}

func TestModerationHiddenListingDoesNotProject(t *testing.T) {
	p, listings, _ := mustProjector(t)
	id := mustID(t)
	listings.put(publishedSnapshot(id, "Bike", nil))
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	restricted := publishedSnapshot(id, "Bike", nil)
	restricted.ModerationState = listingcontracts.ModerationStateRestricted
	listings.put(restricted)
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), id); !errors.Is(err, errNotFound) {
		t.Fatalf("restricted get err = %v", err)
	}
	removed := publishedSnapshot(id, "Bike", nil)
	removed.ModerationState = listingcontracts.ModerationStateRemoved
	listings.put(removed)
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), id); !errors.Is(err, errNotFound) {
		t.Fatalf("removed get err = %v", err)
	}
}

func TestUpdatedEventRemovesRestrictedListing(t *testing.T) {
	h, p, listings, _ := mustHandler(t)
	id := mustID(t)
	listings.put(publishedSnapshot(id, "Bike", nil))
	if err := h.Handle(context.Background(), outboxEvent(t, listingcontracts.EventTypePublished, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	hidden := publishedSnapshot(id, "Bike", nil)
	hidden.ModerationState = listingcontracts.ModerationStateRestricted
	listings.put(hidden)
	if err := h.Handle(context.Background(), outboxEvent(t, listingcontracts.EventTypeUpdated, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), id); !errors.Is(err, errNotFound) {
		t.Fatalf("updated restrict err = %v", err)
	}
	if err := h.Handle(context.Background(), outboxEvent(t, listingcontracts.EventTypeUpdated, id)); err != nil {
		t.Fatal(err)
	}
}

func TestModerationRestoreProjectsWhenPublished(t *testing.T) {
	h, p, listings, _ := mustHandler(t)
	id := mustID(t)
	hidden := publishedSnapshot(id, "Bike", nil)
	hidden.ModerationState = listingcontracts.ModerationStateRemoved
	listings.put(hidden)
	if err := h.Handle(context.Background(), outboxEvent(t, listingcontracts.EventTypeUpdated, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), id); !errors.Is(err, errNotFound) {
		t.Fatalf("removed get err = %v", err)
	}
	restored := publishedSnapshot(id, "Bike", nil)
	restored.ModerationState = listingcontracts.ModerationStateNone
	listings.put(restored)
	if err := h.Handle(context.Background(), outboxEvent(t, listingcontracts.EventTypeUpdated, id)); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get(context.Background(), id)
	if err != nil || got.Title != "Bike" {
		t.Fatalf("restored doc = %+v err=%v", got, err)
	}
}

func TestArchivedListingStaysUnsearchableAfterModerationClear(t *testing.T) {
	p, listings, _ := mustProjector(t)
	id := mustID(t)
	listings.put(publishedSnapshot(id, "Bike", nil))
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	archivedRemoved := publishedSnapshot(id, "Bike", nil)
	archivedRemoved.Status = "archived"
	archivedRemoved.ModerationState = listingcontracts.ModerationStateRemoved
	listings.put(archivedRemoved)
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	cleared := archivedRemoved
	cleared.ModerationState = listingcontracts.ModerationStateNone
	listings.put(cleared)
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), id); !errors.Is(err, errNotFound) {
		t.Fatalf("archived restore get err = %v", err)
	}
}

func TestArchiveRemovesProjection(t *testing.T) {
	p, listings, _ := mustProjector(t)
	id := mustID(t)
	listings.put(publishedSnapshot(id, "Bike", nil))
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	arch := publishedSnapshot(id, "Bike", nil)
	arch.Status = "archived"
	listings.put(arch)
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), id); !errors.Is(err, errNotFound) {
		t.Fatalf("get err = %v", err)
	}
}

func TestLocationChangeUpdatesGeo(t *testing.T) {
	p, listings, geo := mustProjector(t)
	id := mustID(t)
	listings.put(publishedSnapshot(id, "Bike", nil))
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	lat, lon := 36.7, 29.2
	geo.point = &locationcontracts.ListingPoint{
		ListingID: locationcontracts.ID(id),
		Latitude:  lat,
		Longitude: lon,
	}
	if err := p.Rebuild(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get(context.Background(), id)
	if err != nil || got.Latitude == nil || *got.Latitude != lat || got.Longitude == nil || *got.Longitude != lon {
		t.Fatalf("geo = %+v err = %v", got, err)
	}
}

func TestReplayedEventIsIdempotent(t *testing.T) {
	h, p, listings, _ := mustHandler(t)
	id := mustID(t)
	listings.put(publishedSnapshot(id, "Bike", nil))
	ev := outboxEvent(t, listingcontracts.EventTypePublished, id)
	if err := h.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get(context.Background(), id)
	if err != nil || got.Title != "Bike" {
		t.Fatalf("got = %+v err = %v", got, err)
	}
}

func TestMalformedAndUnknownSourceFailSafely(t *testing.T) {
	h, p, listings, _ := mustHandler(t)
	if err := h.Handle(context.Background(), outbox.Event{
		EventType:    listingcontracts.EventTypePublished,
		EventVersion: listingcontracts.EventVersion,
		Payload:      json.RawMessage(`{"listing_id":"not-a-uuid"}`),
	}); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("malformed err = %v", err)
	}
	if err := h.Handle(context.Background(), outbox.Event{
		EventType:    "search.unknown",
		EventVersion: 1,
		Payload:      json.RawMessage(`{"listing_id":"` + mustID(t).String() + `"}`),
	}); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("unknown type err = %v", err)
	}
	missing := mustID(t)
	if err := h.Handle(context.Background(), outboxEvent(t, listingcontracts.EventTypeUpdated, missing)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), missing); !errors.Is(err, errNotFound) {
		t.Fatalf("missing source must not project: %v", err)
	}
	id := mustID(t)
	listings.put(publishedSnapshot(id, "Bike", nil))
	if err := h.Handle(context.Background(), outboxEvent(t, listingcontracts.EventTypePublished, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	_ = listings
}

func TestLocationChangedRebuildsPublishedListing(t *testing.T) {
	h, p, listings, geo := mustHandler(t)
	id := mustID(t)
	listings.put(publishedSnapshot(id, "Bike", nil))
	lat, lon := 36.621, 29.116
	geo.point = &locationcontracts.ListingPoint{ListingID: locationcontracts.ID(id), Latitude: lat, Longitude: lon}
	if err := h.Handle(context.Background(), outboxEvent(t, locationcontracts.EventTypeListingChanged, id)); err != nil {
		t.Fatal(err)
	}
	got, err := p.Get(context.Background(), id)
	if err != nil || got.Latitude == nil || *got.Latitude != lat {
		t.Fatalf("got = %+v err = %v", got, err)
	}
}

func TestNewProjectorRequiresSources(t *testing.T) {
	if _, err := NewProjector(nil, &stubListings{}, &stubGeo{}); !errors.Is(err, errStoreRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewProjector(NewMemoryStore(), nil, &stubGeo{}); !errors.Is(err, errSourceRequired) {
		t.Fatalf("err = %v", err)
	}
}

type stubListings struct {
	byID map[listingcontracts.ID]listingcontracts.ListingSnapshot
	err  error
}

func (s *stubListings) put(snap listingcontracts.ListingSnapshot) {
	if s.byID == nil {
		s.byID = map[listingcontracts.ID]listingcontracts.ListingSnapshot{}
	}
	s.byID[snap.ID] = snap
}

func (s *stubListings) GetListingSnapshot(_ context.Context, listingID listingcontracts.ID) (listingcontracts.ListingSnapshot, error) {
	if s.err != nil {
		return listingcontracts.ListingSnapshot{}, s.err
	}
	snap, ok := s.byID[listingID]
	if !ok {
		return listingcontracts.ListingSnapshot{}, listingcontracts.ErrNotFound
	}
	return snap, nil
}

type stubGeo struct {
	got   locationcontracts.ID
	point *locationcontracts.ListingPoint
	err   error
}

func (s *stubGeo) GetListingLocation(_ context.Context, listingID locationcontracts.ID) (locationcontracts.ListingPoint, error) {
	s.got = listingID
	if s.err != nil {
		return locationcontracts.ListingPoint{}, s.err
	}
	if s.point != nil {
		return *s.point, nil
	}
	return locationcontracts.ListingPoint{}, locationcontracts.ErrNotFound
}

func mustProjector(t *testing.T) (*Projector, *stubListings, *stubGeo) {
	t.Helper()
	listings := &stubListings{}
	geo := &stubGeo{}
	p, err := NewProjector(NewMemoryStore(), listings, geo)
	if err != nil {
		t.Fatal(err)
	}
	return p, listings, geo
}

func mustHandler(t *testing.T) (*ProjectionHandler, *Projector, *stubListings, *stubGeo) {
	t.Helper()
	p, listings, geo := mustProjector(t)
	h, err := NewProjectionHandler(p)
	if err != nil {
		t.Fatal(err)
	}
	return h, p, listings, geo
}

func publishedSnapshot(id ID, title string, publishedAt *time.Time) listingcontracts.ListingSnapshot {
	now := time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)
	if publishedAt == nil {
		t := now
		publishedAt = &t
	}
	return listingcontracts.ListingSnapshot{
		ID:                    listingcontracts.ID(id),
		Status:                listingcontracts.StatusPublished,
		CategoryID:            listingcontracts.ID(id),
		CategorySchemaVersion: 1,
		Title:                 title,
		Description:           "Used bicycle",
		Attributes:            map[string]any{"condition": "used"},
		PublishedAt:           publishedAt,
		UpdatedAt:             now,
	}
}

func mustDocFromSnap(t *testing.T, snap listingcontracts.ListingSnapshot) ListingDocument {
	t.Helper()
	doc, err := documentFromSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func outboxEvent(t *testing.T, eventType string, id ID) outbox.Event {
	t.Helper()
	payload, err := json.Marshal(listingcontracts.ListingIDPayload{ListingID: id.String()})
	if err != nil {
		t.Fatal(err)
	}
	version := listingcontracts.EventVersion
	if eventType == locationcontracts.EventTypeListingChanged {
		version = locationcontracts.EventVersion
	}
	return outbox.Event{
		EventType:    eventType,
		EventVersion: version,
		Payload:      payload,
	}
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
