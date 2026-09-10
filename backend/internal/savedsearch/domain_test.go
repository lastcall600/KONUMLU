package savedsearch

import (
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

func TestSavedSearchValidate(t *testing.T) {
	id, user := mustID(t), mustID(t)
	row := SavedSearch{ID: id, UserID: user, Name: "Fethiye bisiklet", CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}
	if err := row.Validate(); err != nil {
		t.Fatal(err)
	}
	row.Name = ""
	if err := row.Validate(); !errors.Is(err, errInvalid) {
		t.Fatalf("empty name err = %v", err)
	}
}

func TestFiltersRejectPartialViewportAndBadPrices(t *testing.T) {
	f := Filters{Viewport: &Viewport{North: 37, South: 36, East: 30, West: 28}}
	if err := f.Normalize(); err != nil {
		t.Fatal(err)
	}
	bad := "x"
	f = Filters{MinPrice: &bad}
	if err := f.Normalize(); !errors.Is(err, errInvalid) {
		t.Fatalf("price err = %v", err)
	}
	min, max := "90", "10"
	f = Filters{MinPrice: &min, MaxPrice: &max}
	if err := f.Normalize(); !errors.Is(err, errInvalid) {
		t.Fatalf("range err = %v", err)
	}
	cur := "try"
	f = Filters{Currency: &cur}
	if err := f.Normalize(); !errors.Is(err, errInvalid) {
		t.Fatalf("currency err = %v", err)
	}
	long := strings.Repeat("a", maxQueryLen+1)
	f = Filters{Q: &long}
	if err := f.Normalize(); !errors.Is(err, errInvalid) {
		t.Fatalf("q err = %v", err)
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
