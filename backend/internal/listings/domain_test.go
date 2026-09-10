package listings

import (
	"errors"
	"testing"
	"time"
)

func TestNewDraft(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	owner := mustID(t)
	cat := mustID(t)
	got, err := NewDraft(owner, validContent(cat), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDraft || got.OwnerUserID != owner || got.PublishedAt != nil {
		t.Fatalf("listing = %+v", got)
	}
	if got.Title != "Bike" || got.Description != "Used bicycle" {
		t.Fatal("title and description must remain original user content")
	}
}

func TestInvalidStatusTransition(t *testing.T) {
	listing := mustDraft(t)
	listing.Status = StatusPublished
	at := listing.CreatedAt.Add(time.Minute)
	if _, err := listing.MarkReady(at); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("ready err = %v", err)
	}
	if _, err := listing.UpdateDraft(validContent(listing.CategoryID), at); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("update err = %v", err)
	}
}

func TestReadyTransition(t *testing.T) {
	listing := mustDraft(t)
	got, err := listing.MarkReady(listing.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusReady || got.PublishedAt != nil {
		t.Fatalf("listing = %+v", got)
	}
}

func TestArchive(t *testing.T) {
	listing := mustDraft(t)
	got, err := listing.Archive(listing.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusArchived || got.ArchivedAt == nil {
		t.Fatalf("listing = %+v", got)
	}
	if got.ModerationState.Normalized() != ModerationNone {
		t.Fatalf("archive must not set moderation state: %+v", got)
	}
	if _, err := got.Archive(got.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("double archive err = %v", err)
	}
}

func TestPublishEligibilityRequired(t *testing.T) {
	listing := mustDraft(t)
	ready, err := listing.MarkReady(listing.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ready.Publish(ready.UpdatedAt.Add(time.Second), false); !errors.Is(err, errPublishNotEligible) {
		t.Fatalf("ineligible err = %v", err)
	}
	if _, err := listing.Publish(listing.CreatedAt.Add(time.Minute), true); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("draft publish err = %v", err)
	}
	got, err := ready.Publish(ready.UpdatedAt.Add(time.Second), true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPublished || got.PublishedAt == nil {
		t.Fatalf("listing = %+v", got)
	}
}

func TestApplyModerationDoesNotArchive(t *testing.T) {
	listing := mustDraft(t)
	ready, err := listing.MarkReady(listing.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	published, err := ready.Publish(ready.UpdatedAt.Add(time.Second), true)
	if err != nil {
		t.Fatal(err)
	}
	restricted, err := published.ApplyModeration(ModerationRestricted, published.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if restricted.Status != StatusPublished || restricted.ArchivedAt != nil {
		t.Fatalf("restrict must keep owner lifecycle: %+v", restricted)
	}
	if restricted.ModerationState != ModerationRestricted || restricted.PubliclyReadable() {
		t.Fatalf("restrict visibility = %+v", restricted)
	}
	removed, err := restricted.ApplyModeration(ModerationRemoved, restricted.UpdatedAt.Add(time.Second))
	if err != nil || removed.Status != StatusPublished || removed.ModerationState != ModerationRemoved {
		t.Fatalf("remove = %+v err=%v", removed, err)
	}
	if _, err := published.ApplyModeration(ModerationNone, published.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidModeration) {
		t.Fatalf("none restore err = %v", err)
	}
	cleared, err := removed.ClearModeration(removed.UpdatedAt.Add(time.Second))
	if err != nil || cleared.Status != StatusPublished || cleared.ModerationState != ModerationNone || !cleared.PubliclyReadable() {
		t.Fatalf("clear published = %+v err=%v", cleared, err)
	}
	archived, err := published.Archive(published.UpdatedAt.Add(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	hiddenArchived, err := archived.ApplyModeration(ModerationRemoved, archived.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	restoredArchived, err := hiddenArchived.ClearModeration(hiddenArchived.UpdatedAt.Add(time.Second))
	if err != nil || restoredArchived.Status != StatusArchived || restoredArchived.ModerationState != ModerationNone {
		t.Fatalf("clear archived = %+v err=%v", restoredArchived, err)
	}
	if restoredArchived.PubliclyReadable() || restoredArchived.ArchivedAt == nil {
		t.Fatalf("clear must not unarchive: %+v", restoredArchived)
	}
	again, err := restoredArchived.ClearModeration(restoredArchived.UpdatedAt.Add(time.Second))
	if err != nil || again.ModerationState != ModerationNone || again.Status != StatusArchived {
		t.Fatalf("idempotent clear = %+v err=%v", again, err)
	}
}

func TestControlledAttributesValidationShape(t *testing.T) {
	if err := ValidateAttributesShape(Attributes{"condition": "used", "year": 2018, "tags": []any{"city"}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAttributesShape(Attributes{}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAttributesShape(nil); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("nil err = %v", err)
	}
	if err := ValidateAttributesShape(Attributes{"Bad Key": "x"}); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("key err = %v", err)
	}
	if err := ValidateAttributesShape(Attributes{"nested": map[string]any{"a": 1}}); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("object err = %v", err)
	}
}

func mustDraft(t *testing.T) Listing {
	t.Helper()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	listing, err := NewDraft(mustID(t), validContent(mustID(t)), now)
	if err != nil {
		t.Fatal(err)
	}
	return listing
}

func validContent(categoryID ID) DraftContent {
	return DraftContent{
		CategoryID:            categoryID,
		CategorySchemaVersion: 1,
		Title:                 "Bike",
		Description:           "Used bicycle",
		Attributes:            Attributes{"condition": "used"},
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
