package reviews

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseIDRejectsZeroAndGarbage(t *testing.T) {
	if _, err := ParseID(""); !errors.Is(err, errZeroID) {
		t.Fatalf("empty err = %v", err)
	}
	if _, err := ParseID("not-a-uuid"); !errors.Is(err, errZeroID) {
		t.Fatalf("garbage err = %v", err)
	}
	if _, err := ParseID("00000000-0000-0000-0000-000000000000"); !errors.Is(err, errZeroID) {
		t.Fatalf("zero err = %v", err)
	}
}

func TestNormalizeBodyOptionalAndOversize(t *testing.T) {
	got, err := NormalizeBody("   ")
	if err != nil || got != nil {
		t.Fatalf("empty got = %v err=%v", got, err)
	}
	got, err = NormalizeBody("  ok  ")
	if err != nil || got == nil || *got != "ok" {
		t.Fatalf("trim got = %v err=%v", got, err)
	}
	if _, err := NormalizeBody(strings.Repeat("a", MaxBodyBytes+1)); !errors.Is(err, errInvalidBody) {
		t.Fatalf("oversize err = %v", err)
	}
}

func TestValidateRatingRange(t *testing.T) {
	if err := ValidateRating(0); !errors.Is(err, errInvalidRating) {
		t.Fatalf("0 err = %v", err)
	}
	if err := ValidateRating(6); !errors.Is(err, errInvalidRating) {
		t.Fatalf("6 err = %v", err)
	}
	if err := ValidateRating(1); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRating(5); err != nil {
		t.Fatal(err)
	}
}

func TestReviewKeepsRatingDimensionsSeparate(t *testing.T) {
	id := mustID(t)
	reviewer := mustID(t)
	now := time.Unix(10, 0).UTC()
	row := Review{
		ID: id, VerifiedInteractionID: mustID(t), ListingID: mustID(t),
		ReviewerUserID: reviewer, ProviderUserID: mustID(t),
		ListingAccuracy: 5, ProviderService: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := row.Validate(); err != nil {
		t.Fatal(err)
	}
	if row.ListingAccuracy == row.ProviderService {
		t.Fatal("dimensions must remain independent")
	}
}

func TestEncodeVerifiedCreatedOmitsBody(t *testing.T) {
	body := "secret feedback"
	now := time.Unix(20, 0).UTC()
	row := Review{
		ID: mustID(t), VerifiedInteractionID: mustID(t), ListingID: mustID(t),
		ReviewerUserID: mustID(t), ProviderUserID: mustID(t),
		Body: &body, ListingAccuracy: 4, ProviderService: 2,
		CreatedAt: now, UpdatedAt: now,
	}
	ev, err := encodeVerifiedCreated(row)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ev.Payload), "body") || strings.Contains(string(ev.Payload), body) {
		t.Fatalf("body leaked: %s", ev.Payload)
	}
	payload, err := DecodeVerifiedCreated(ev.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if payload.ListingAccuracy != 4 || payload.ProviderService != 2 {
		t.Fatalf("ratings = %+v", payload)
	}
	var raw map[string]any
	if err := json.Unmarshal(ev.Payload, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["body"]; ok {
		t.Fatal("body field present")
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
