package masterdata

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestNewCategoryDraft(t *testing.T) {
	now := time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)
	got, err := NewCategory("vehicles.bike", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDraft || got.PublishedAt != nil || got.Code != "vehicles.bike" {
		t.Fatalf("category = %+v", got)
	}
	if got.EIDSRequirement != EIDSRequirementNone {
		t.Fatalf("eids default = %s", got.EIDSRequirement)
	}
}

func TestCategoryLifecycleTransitions(t *testing.T) {
	c, err := NewCategory("goods", nil, time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	now := c.CreatedAt.Add(time.Minute)
	if _, err := c.Approve(now); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("approve from draft err = %v", err)
	}
	if _, err := c.Publish(now); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("publish from draft err = %v", err)
	}
	review, err := c.SubmitForReview(now)
	if err != nil || review.Status != StatusReview {
		t.Fatalf("review = %+v err = %v", review, err)
	}
	if _, err := review.SubmitForReview(now.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("double review err = %v", err)
	}
	approved, err := review.Approve(now.Add(time.Minute))
	if err != nil || approved.Status != StatusApproved {
		t.Fatalf("approved = %+v err = %v", approved, err)
	}
	published, err := approved.Publish(now.Add(2 * time.Minute))
	if err != nil || published.Status != StatusPublished || published.PublishedAt == nil {
		t.Fatalf("published = %+v err = %v", published, err)
	}
}

func TestParseLocale(t *testing.T) {
	got, err := ParseLocale("")
	if err != nil || got != LocaleTR {
		t.Fatalf("empty = %q err = %v", got, err)
	}
	got, err = ParseLocale(" ar ")
	if err != nil || got != LocaleAR {
		t.Fatalf("ar = %q err = %v", got, err)
	}
	if _, err := ParseLocale("TR"); !errors.Is(err, errInvalidLocale) {
		t.Fatalf("TR err = %v", err)
	}
}

func TestInvalidCodeRejected(t *testing.T) {
	now := time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)
	for _, code := range []string{"", "Bike", "ıslan", "has space", "-lead"} {
		if _, err := NewCategory(code, nil, now); !errors.Is(err, errInvalidCode) {
			t.Fatalf("code %q err = %v", code, err)
		}
	}
}

func TestSchemaVersionPublishImmutable(t *testing.T) {
	now := time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)
	s, err := NewSchema(mustID(t), 1, now)
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.SubmitForReview(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	approved, err := review.Approve(now.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	published, err := approved.Publish(now.Add(3 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := published.SubmitForReview(now.Add(4 * time.Minute)); !errors.Is(err, errImmutablePublished) {
		t.Fatalf("mutate published err = %v", err)
	}
	if err := published.assertDraft(); !errors.Is(err, errImmutablePublished) {
		t.Fatalf("assert draft err = %v", err)
	}
}

func TestAttributeValidation(t *testing.T) {
	schemaID := mustID(t)
	ok, err := NewAttribute(schemaID, "year", ValueTypeInteger, true, true, true, 1, Constraints{"min": 1900})
	if err != nil {
		t.Fatal(err)
	}
	if !ok.Required || !ok.Filterable || !ok.Sortable {
		t.Fatalf("flags = %+v", ok)
	}
	if _, err := NewAttribute(schemaID, "used", ValueTypeBoolean, false, true, true, 0, Constraints{}); !errors.Is(err, errInvalidAttribute) {
		t.Fatalf("sortable boolean err = %v", err)
	}
	if _, err := NewAttribute(schemaID, "condition", ValueTypeEnum, true, true, true, 0, Constraints{}); !errors.Is(err, errInvalidAttribute) {
		t.Fatalf("sortable enum err = %v", err)
	}
	if _, err := NewAttribute(schemaID, "nested", ValueTypeText, false, false, false, 0, Constraints{"bad": map[string]any{"x": 1}}); !errors.Is(err, errInvalidConstraints) {
		t.Fatalf("nested constraints err = %v", err)
	}
	if err := ValidateConstraints(nil); !errors.Is(err, errInvalidConstraints) {
		t.Fatalf("nil constraints err = %v", err)
	}
}

func TestTurkishLocaleDoesNotCorruptDottedI(t *testing.T) {
	label := "İlan Işık ıI"
	got, err := NewCategoryLabel(mustID(t), LocaleTR, label, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != label {
		t.Fatalf("stored %q want %q", got.Label, label)
	}
	if strings.ToLower(label) == TurkishFold(label) {
		t.Fatal("unicode default ToLower must not equal Turkish fold for İ/I")
	}
	if TurkishFold("I") != "ı" {
		t.Fatalf("I fold = %q", TurkishFold("I"))
	}
	if TurkishFold("İ") != "i" {
		t.Fatalf("İ fold = %q", TurkishFold("İ"))
	}
	if unicode.ToLower('I') == 'ı' {
		t.Fatal("test assumption: unicode ToLower(I) is not ı")
	}
	if !EqualTurkishFold("Işık", "ışık") {
		t.Fatal("turkish fold should match Işık/ışık")
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
