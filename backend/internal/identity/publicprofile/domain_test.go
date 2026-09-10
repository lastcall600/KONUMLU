package publicprofile

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"backend/internal/identity"
)

func TestNormalizeDisplayName(t *testing.T) {
	got, err := NormalizeDisplayName("  Ada  ")
	if err != nil || got == nil || *got != "Ada" {
		t.Fatalf("got=%v err=%v", got, err)
	}
	empty, err := NormalizeDisplayName("   ")
	if err != nil || empty != nil {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
	if _, err := NormalizeDisplayName("<script>"); err != ErrInvalidDisplayName {
		t.Fatalf("html err=%v", err)
	}
	if _, err := NormalizeDisplayName("a\nb"); err != ErrInvalidDisplayName {
		t.Fatalf("control err=%v", err)
	}
	long := strings.Repeat("a", MaxDisplayNameRunes+1)
	if utf8.RuneCountInString(long) <= MaxDisplayNameRunes {
		t.Fatal("fixture")
	}
	if _, err := NormalizeDisplayName(long); err != ErrInvalidDisplayName {
		t.Fatalf("long err=%v", err)
	}
}

func TestParsePublicID(t *testing.T) {
	id, err := identity.NewID()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParsePublicID(id.String())
	if err != nil || got != id {
		t.Fatalf("got=%s err=%v", got, err)
	}
	if _, err := ParsePublicID("not-a-uuid"); err != ErrInvalidPublicID {
		t.Fatalf("malformed err=%v", err)
	}
	if _, err := ParsePublicID("00000000-0000-0000-0000-000000000000"); err != ErrInvalidPublicID {
		t.Fatalf("zero err=%v", err)
	}
}

func TestProfileRejectsReusingUserID(t *testing.T) {
	id := mustID(t)
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	p := Profile{UserID: id, PublicProfileID: id, CreatedAt: now, UpdatedAt: now}
	if err := p.Validate(); err != ErrUnavailable {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyAndClearModeration(t *testing.T) {
	id := mustID(t)
	public := mustID(t)
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	p := Profile{UserID: id, PublicProfileID: public, ModerationState: ModerationNone, CreatedAt: now, UpdatedAt: now}
	restricted, err := p.ApplyModeration(ModerationRestricted, now.Add(time.Second))
	if err != nil || restricted.ModerationState != ModerationRestricted {
		t.Fatalf("restricted = %+v err=%v", restricted, err)
	}
	if _, err := p.ApplyModeration(ModerationNone, now.Add(time.Second)); !errors.Is(err, ErrModerationNotEnforced) {
		t.Fatalf("none apply err=%v", err)
	}
	again, err := restricted.ApplyModeration(ModerationRestricted, now.Add(2*time.Second))
	if err != nil || again.ModerationState != ModerationRestricted {
		t.Fatalf("idempotent restrict = %+v err=%v", again, err)
	}
	cleared, err := again.ClearModeration(now.Add(3 * time.Second))
	if err != nil || cleared.ModerationState != ModerationNone {
		t.Fatalf("cleared = %+v err=%v", cleared, err)
	}
	onceMore, err := cleared.ClearModeration(now.Add(4 * time.Second))
	if err != nil || onceMore.ModerationState != ModerationNone {
		t.Fatalf("idempotent clear = %+v err=%v", onceMore, err)
	}
}
