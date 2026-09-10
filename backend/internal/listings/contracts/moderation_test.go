package contracts

import "testing"

func TestPubliclyVisibleTreatsEmptyAsNone(t *testing.T) {
	if !PubliclyVisible(StatusPublished, "") || !PubliclyVisible(StatusPublished, ModerationStateNone) {
		t.Fatal("published none must be public")
	}
	if PubliclyVisible(StatusPublished, ModerationStateRestricted) || PubliclyVisible(StatusPublished, ModerationStateRemoved) {
		t.Fatal("restricted/removed must not be public")
	}
	if PubliclyVisible(StatusArchived, ModerationStateNone) {
		t.Fatal("archived must not be public")
	}
	ref := ListingRef{Status: StatusPublished}
	if !ref.PubliclyVisible() {
		t.Fatal("empty ref moderation is public when published")
	}
}

func TestPubliclyVisibleAfterModerationClearDependsOnOwnerStatus(t *testing.T) {
	if !PubliclyVisible(StatusPublished, ModerationStateNone) {
		t.Fatal("published none must be public")
	}
	if PubliclyVisible(StatusArchived, ModerationStateNone) {
		t.Fatal("archived none must not be public")
	}
	if PubliclyVisible("ready", ModerationStateNone) {
		t.Fatal("ready none must not be public")
	}
}

func TestApplyModerationInputRejectsNone(t *testing.T) {
	var id ID
	id[0] = 1
	in := ApplyModerationInput{ListingID: id, State: ModerationStateRestricted}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	in.State = ModerationStateNone
	if err := in.Validate(); err != ErrModerationNotEnforced {
		t.Fatalf("none err = %v", err)
	}
	in.State = "suspend"
	if err := in.Validate(); err != ErrInvalidModerationState {
		t.Fatalf("suspend err = %v", err)
	}
}
