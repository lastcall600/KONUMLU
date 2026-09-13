package policy

import (
	"errors"
	"testing"
	"time"
)

func TestActorCannotReadAnotherUsersPreferencesOrConsentOrInbox(t *testing.T) {
	s := NewMemoryStore()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	if _, err := s.PatchPreferences("user-a", PreferencePatch{Overrides: []PreferenceOverride{{Channel: ChannelEmail, ScopeType: ScopeChannel, ScopeKey: "*", Enabled: false}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordConsent("user-a", ConsentDecision{
		Type: ConsentCommercialEmail, Grant: true, Source: ConsentSourceSettingsWeb,
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.PutInbox(InboxItem{ID: "n1", UserID: "user-a", EventType: EventOfferReceived, TemplateKey: "offer.received", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	prefsB, err := s.GetPreferences("user-b")
	if err != nil {
		t.Fatal(err)
	}
	if !prefsB.channelOn(ChannelEmail) {
		t.Fatal("user-b must see defaults, not user-a email mute")
	}
	consB, err := s.ListConsents("user-b")
	if err != nil || len(consB) != 0 {
		t.Fatalf("user-b consents = %v %v", consB, err)
	}
	boxB, err := s.ListInbox("user-b")
	if err != nil || len(boxB) != 0 {
		t.Fatalf("user-b inbox = %v %v", boxB, err)
	}
	if _, err := s.MarkRead("user-b", "n1", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mark-read cross-user err=%v", err)
	}
	if err := RejectClientUserID("user-a", "user-b"); !errors.Is(err, ErrInvalidActor) {
		t.Fatal("client body user_id must not authorize")
	}
	if err := RejectClientUserID("user-a", ""); err != nil {
		t.Fatal(err)
	}
}

func TestCannotMutateSystemRequiredPolicy(t *testing.T) {
	err := ValidatePreferencePatch(PreferencePatch{Overrides: []PreferenceOverride{{Channel: ChannelInApp, ScopeType: ScopeChannel, ScopeKey: "*", Enabled: false}}})
	if !errors.Is(err, ErrSystemRequired) {
		t.Fatalf("err=%v", err)
	}
	err = ValidatePreferencePatch(PreferencePatch{Overrides: []PreferenceOverride{{Channel: ChannelEmail, ScopeType: ScopeCategory, ScopeKey: string(CategorySecurity), Enabled: false}}})
	if !errors.Is(err, ErrSystemRequired) {
		t.Fatalf("err=%v", err)
	}
	err = ValidatePreferencePatch(PreferencePatch{Overrides: []PreferenceOverride{{
		Channel: ChannelEmail, ScopeType: ScopeEvent, ScopeKey: string(EventSecurityLoginNew), Enabled: false,
	}}})
	if !errors.Is(err, ErrSystemRequired) {
		t.Fatalf("err=%v", err)
	}
	err = ValidatePreferencePatch(PreferencePatch{Overrides: []PreferenceOverride{{Channel: "firebase", ScopeType: ScopeChannel, ScopeKey: "*", Enabled: true}}})
	if !errors.Is(err, ErrUnknownPreference) {
		t.Fatalf("err=%v", err)
	}
}

func TestEmptyActorRejected(t *testing.T) {
	s := NewMemoryStore()
	if _, err := s.GetPreferences(""); !errors.Is(err, ErrInvalidActor) {
		t.Fatalf("err=%v", err)
	}
	if _, err := s.ListConsents(""); !errors.Is(err, ErrInvalidActor) {
		t.Fatalf("err=%v", err)
	}
	if _, err := s.ListInbox(""); !errors.Is(err, ErrInvalidActor) {
		t.Fatalf("err=%v", err)
	}
}

func TestMarkAllReadIsOwnerScoped(t *testing.T) {
	s := NewMemoryStore()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	_ = s.PutInbox(InboxItem{ID: "a1", UserID: "user-a", EventType: EventOfferReceived, CreatedAt: now})
	_ = s.PutInbox(InboxItem{ID: "b1", UserID: "user-b", EventType: EventOfferReceived, CreatedAt: now})
	if err := s.MarkAllRead("user-a", now); err != nil {
		t.Fatal(err)
	}
	a, _ := s.ListInbox("user-a")
	b, _ := s.ListInbox("user-b")
	if a[0].ReadAt == nil || b[0].ReadAt != nil {
		t.Fatalf("a=%v b=%v", a, b)
	}
}
