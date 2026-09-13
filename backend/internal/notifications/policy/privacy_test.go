package policy

import (
	"errors"
	"testing"
	"time"
)

func TestSignupDoesNotInferMarketingConsent(t *testing.T) {
	if rows := AccountCreatedDoesNotGrantConsent("user-new"); len(rows) != 0 {
		t.Fatalf("inferred consent: %#v", rows)
	}
	s := NewMemoryStore()
	got, err := s.ListConsents("user-new")
	if err != nil || len(got) != 0 {
		t.Fatalf("store after account create simulation: %v %v", got, err)
	}
}

func TestWithdrawalOverridesMarketingPreference(t *testing.T) {
	in := baseInput(EventMarketingCampaign)
	in.Preferences = in.Preferences.Override(ChannelEmail, ScopeCategory, string(CategoryMarketing), true)
	in.Preferences = in.Preferences.Override(ChannelEmail, ScopeChannel, "*", true)
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	in.Consents = []ConsentSnapshot{{
		Type: ConsentCommercialEmail, Status: ConsentWithdrawn, RecordedSeq: 2,
		CapturedAt: now, WithdrawnAt: &now, Source: ConsentSourceSettingsWeb,
	}}
	in.RequestedChannels = []Channel{ChannelEmail}
	got := Resolve(in)
	if len(got.EligibleChannels) != 0 {
		t.Fatalf("eligible=%v", got.EligibleChannels)
	}
	if suppression(got, ChannelEmail) != SuppressConsentWithdrawn {
		t.Fatalf("reason=%s", suppression(got, ChannelEmail))
	}
}

func TestConsentRejectsClientForgedTimestampAndArbitraryType(t *testing.T) {
	ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := ValidateConsentDecision(ConsentDecision{
		Type: ConsentCommercialEmail, Grant: true, Source: ConsentSourceSettingsWeb, ClientCapturedAt: &ts,
	}, time.Now().UTC())
	if !errors.Is(err, ErrClientTimestamp) {
		t.Fatalf("err=%v", err)
	}
	_, err = ValidateConsentDecision(ConsentDecision{
		Type: "kvkk.always_ok", Grant: true, Source: ConsentSourceSettingsWeb,
	}, time.Now().UTC())
	if !errors.Is(err, ErrUnknownConsent) {
		t.Fatalf("err=%v", err)
	}
}

func TestConsentVersionIncrementsOnWithdrawal(t *testing.T) {
	s := NewMemoryStore()
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	first, err := s.RecordConsent("u1", ConsentDecision{Type: ConsentCommercialEmail, Grant: true, Source: ConsentSourceSettingsWeb}, now)
	if err != nil || first.RecordedSeq != 1 || first.Status != ConsentGranted {
		t.Fatalf("%+v %v", first, err)
	}
	second, err := s.RecordConsent("u1", ConsentDecision{Type: ConsentCommercialEmail, Grant: false, Source: ConsentSourceSettingsWeb}, now.Add(time.Hour))
	if err != nil || second.RecordedSeq != 2 || second.Status != ConsentWithdrawn || second.WithdrawnAt == nil {
		t.Fatalf("%+v %v", second, err)
	}
}

func TestDisabledAccountStillGetsSecurity(t *testing.T) {
	in := baseInput(EventSecurityPasswordResetCompleted)
	in.Account = AccountDisabled
	got := Resolve(in)
	on := eligibleSet(got)
	if !on[ChannelInApp] || !on[ChannelEmail] {
		t.Fatalf("security should remain eligible: %v", got.Decisions)
	}
	in2 := baseInput(EventMarketingCampaign)
	in2.Account = AccountDisabled
	in2.Preferences = in2.Preferences.Override(ChannelEmail, ScopeCategory, string(CategoryMarketing), true)
	got2 := Resolve(in2)
	if len(got2.EligibleChannels) != 0 {
		t.Fatalf("marketing on disabled account: %v", got2.EligibleChannels)
	}
}
