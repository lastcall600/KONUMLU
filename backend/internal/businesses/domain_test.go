package businesses

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNewDraftPreservesUnicodeText(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	owner := mustID(t)
	got, err := NewDraft(owner, ProfileContent{
		DisplayName: "  Кафе البحر  ",
		Description: "  Açıklama — описание  ",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDraft || got.OwnerUserID != owner {
		t.Fatalf("profile = %+v", got)
	}
	if got.DisplayName != "Кафе البحر" || got.Description != "Açıklama — описание" {
		t.Fatalf("text = %q / %q", got.DisplayName, got.Description)
	}
}

func TestNewDraftRejectsEmptyAndOverlong(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	owner := mustID(t)
	if _, err := NewDraft(owner, ProfileContent{DisplayName: "  "}, now); !errors.Is(err, errInvalidContent) {
		t.Fatalf("blank err = %v", err)
	}
	if _, err := NewDraft(owner, ProfileContent{DisplayName: strings.Repeat("a", MaxDisplayNameRunes+1)}, now); !errors.Is(err, errInvalidContent) {
		t.Fatalf("name len err = %v", err)
	}
	if _, err := NewDraft(owner, ProfileContent{
		DisplayName: "Cafe",
		Description: strings.Repeat("я", MaxDescriptionRunes+1),
	}, now); !errors.Is(err, errInvalidContent) {
		t.Fatalf("desc len err = %v", err)
	}
	ok, err := NewDraft(owner, ProfileContent{
		DisplayName: strings.Repeat("م", MaxDisplayNameRunes),
		Description: strings.Repeat("б", MaxDescriptionRunes),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(ok.DisplayName) != MaxDisplayNameRunes {
		t.Fatalf("name runes = %d", utf8.RuneCountInString(ok.DisplayName))
	}
}

func TestActivateAndClose(t *testing.T) {
	p := mustDraft(t)
	at := p.CreatedAt.Add(time.Minute)
	active, err := p.Activate(at)
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != StatusActive {
		t.Fatalf("status = %s", active.Status)
	}
	if _, err := active.Activate(at.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("double activate err = %v", err)
	}
	closed, err := active.Close(at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != StatusClosed {
		t.Fatalf("status = %s", closed.Status)
	}
	if _, err := closed.Activate(closed.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
	if _, err := closed.Close(closed.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("double close err = %v", err)
	}
	if _, err := closed.UpdateContent(ProfileContent{DisplayName: "X"}, closed.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("patch closed err = %v", err)
	}
}

func TestCloseDraft(t *testing.T) {
	p := mustDraft(t)
	got, err := p.Close(p.CreatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusClosed {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestSuspendedNotOwnerEditable(t *testing.T) {
	p := mustDraft(t)
	p.Status = StatusSuspended
	if _, err := p.Activate(p.CreatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("activate suspended err = %v", err)
	}
	if _, err := p.Close(p.CreatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("close suspended err = %v", err)
	}
	if _, err := p.UpdateContent(ProfileContent{DisplayName: "X"}, p.CreatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("patch suspended err = %v", err)
	}
	if p.PubliclyReadable() {
		t.Fatal("suspended must not be public")
	}
}

func TestPubliclyReadableOnlyActive(t *testing.T) {
	p := mustDraft(t)
	if p.PubliclyReadable() {
		t.Fatal("draft public")
	}
	active, err := p.Activate(p.CreatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !active.PubliclyReadable() {
		t.Fatal("active should be public")
	}
}

func TestInvalidStatus(t *testing.T) {
	p := mustDraft(t)
	p.Status = "published"
	if err := p.Validate(); !errors.Is(err, errInvalidStatus) {
		t.Fatalf("err = %v", err)
	}
}

func mustDraft(t *testing.T) Profile {
	t.Helper()
	p, err := NewDraft(mustID(t), ProfileContent{DisplayName: "Kafe", Description: "Sahil"}, time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
