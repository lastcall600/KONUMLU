package policy

import (
	"errors"
	"testing"
	"time"
)

func TestPurposeIsNotChannel(t *testing.T) {
	spec := MustEvent(EventMarketingCampaign)
	if spec.Purpose == Purpose("") || len(spec.AllowedChannels) == 0 {
		t.Fatal("catalog")
	}
	for _, ch := range spec.AllowedChannels {
		if string(spec.Purpose) == string(ch) {
			t.Fatalf("purpose collapsed into channel: %s", ch)
		}
	}
}

func TestUnknownEventRejected(t *testing.T) {
	got := Resolve(IntentInput{EventType: "client.invented", RecipientUserID: "u1"})
	if !got.UnknownEvent {
		t.Fatal("expected unknown")
	}
	if _, ok := LookupEvent("firebase.campaign"); ok {
		t.Fatal("vendor event names are not catalog types")
	}
}

func baseInput(event EventType) IntentInput {
	return IntentInput{
		RecipientUserID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		EventType:       event,
		DomainRef:       "ref-1",
		CreatedAt:       time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
		Account:         AccountActive,
		Destinations: DestinationState{
			EmailVerified:   true,
			PhoneVerified:   true,
			WebPushReady:    true,
			MobilePushReady: true,
		},
		Preferences: DefaultPreferences(),
	}
}

func eligibleSet(r Resolution) map[Channel]bool {
	m := map[Channel]bool{}
	for _, ch := range r.EligibleChannels {
		m[ch] = true
	}
	return m
}

func suppression(r Resolution, ch Channel) SuppressionReason {
	for _, d := range r.Decisions {
		if d.Channel == ch {
			return d.Suppressed
		}
	}
	return ""
}

func TestPolicyMatrix(t *testing.T) {
	granted := []ConsentSnapshot{{
		Type: ConsentCommercialEmail, Status: ConsentGranted, RecordedSeq: 1,
		PolicyVersion: PolicyDocumentVersion, CapturedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Source: ConsentSourceSettingsWeb,
	}, {
		Type: ConsentCommercialSMS, Status: ConsentGranted, RecordedSeq: 2,
		PolicyVersion: PolicyDocumentVersion, CapturedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Source: ConsentSourceSettingsWeb,
	}, {
		Type: ConsentCommercialPush, Status: ConsentGranted, RecordedSeq: 3,
		PolicyVersion: PolicyDocumentVersion, CapturedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Source: ConsentSourceSettingsWeb,
	}}
	withdrawnEmail := append([]ConsentSnapshot{}, granted...)
	withdrawnEmail[0].Status = ConsentWithdrawn
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	withdrawnEmail[0].WithdrawnAt = &now
	withdrawnEmail[0].RecordedSeq = 4

	run := []struct {
		name    string
		in      IntentInput
		wantOn  []Channel
		wantOff map[Channel]SuppressionReason
	}{
		{
			name: "security ignores marketing consent and email preference",
			in: func() IntentInput {
				in := baseInput(EventSecurityPasskeyAdded)
				in.Preferences = in.Preferences.Override(ChannelEmail, ScopeChannel, "*", false)
				in.Preferences = in.Preferences.Override(ChannelEmail, ScopeCategory, string(CategoryMarketing), false)
				return in
			}(),
			wantOn: []Channel{ChannelInApp, ChannelEmail},
		},
		{
			name: "transactional in-app remains when product category muted",
			in: func() IntentInput {
				in := baseInput(EventTransactionCreated)
				in.Preferences = in.Preferences.Override(ChannelEmail, ScopeCategory, string(CategorySavedSearches), false)
				in.Preferences = in.Preferences.Override(ChannelWebPush, ScopeChannel, "*", false)
				return in
			}(),
			wantOn:  []Channel{ChannelInApp, ChannelEmail, ChannelMobilePush},
			wantOff: map[Channel]SuppressionReason{ChannelWebPush: SuppressUserPreference},
		},
		{
			name: "social push off in-app on",
			in: func() IntentInput {
				in := baseInput(EventMessagingMessageReceived)
				in.Preferences = in.Preferences.Override(ChannelMobilePush, ScopeCategory, string(CategoryMessages), false)
				in.Preferences = in.Preferences.Override(ChannelInApp, ScopeCategory, string(CategoryMessages), true)
				in.Preferences = in.Preferences.Override(ChannelWebPush, ScopeChannel, "*", false)
				return in
			}(),
			wantOn: []Channel{ChannelInApp},
			wantOff: map[Channel]SuppressionReason{
				ChannelWebPush:    SuppressUserPreference,
				ChannelMobilePush: SuppressUserPreference,
			},
		},
		{
			name: "marketing missing consent",
			in:   baseInput(EventMarketingCampaign),
			wantOff: map[Channel]SuppressionReason{
				ChannelEmail:      SuppressConsentMissing,
				ChannelSMS:        SuppressConsentMissing,
				ChannelWebPush:    SuppressConsentMissing,
				ChannelMobilePush: SuppressConsentMissing,
			},
		},
		{
			name: "marketing withdrawn",
			in: func() IntentInput {
				in := baseInput(EventMarketingCampaign)
				in.Consents = withdrawnEmail
				in.Preferences = in.Preferences.Override(ChannelEmail, ScopeCategory, string(CategoryMarketing), true)
				return in
			}(),
			wantOff: map[Channel]SuppressionReason{ChannelEmail: SuppressConsentWithdrawn},
		},
		{
			name: "marketing consent plus preference on",
			in: func() IntentInput {
				in := baseInput(EventMarketingCampaign)
				in.Consents = granted
				in.Preferences = in.Preferences.Override(ChannelEmail, ScopeCategory, string(CategoryMarketing), true)
				in.Preferences = in.Preferences.Override(ChannelSMS, ScopeCategory, string(CategoryMarketing), true)
				in.Preferences = in.Preferences.Override(ChannelWebPush, ScopeCategory, string(CategoryMarketing), true)
				in.Preferences = in.Preferences.Override(ChannelMobilePush, ScopeCategory, string(CategoryMarketing), true)
				in.Preferences = in.Preferences.Override(ChannelInApp, ScopeCategory, string(CategoryMarketing), true)
				in.Preferences = in.Preferences.Override(ChannelSMS, ScopeChannel, "*", true)
				return in
			}(),
			wantOn: []Channel{ChannelEmail, ChannelSMS, ChannelWebPush, ChannelMobilePush, ChannelInApp},
		},
		{
			name: "marketing consent on preference off",
			in: func() IntentInput {
				in := baseInput(EventMarketingCampaign)
				in.Consents = granted
				return in
			}(),
			wantOff: map[Channel]SuppressionReason{
				ChannelEmail:      SuppressUserPreference,
				ChannelSMS:        SuppressUserPreference,
				ChannelWebPush:    SuppressUserPreference,
				ChannelMobilePush: SuppressUserPreference,
			},
		},
		{
			name: "sms eligible policy without verified phone",
			in: func() IntentInput {
				in := baseInput(EventMarketingCampaign)
				in.Consents = granted
				in.Preferences = in.Preferences.Override(ChannelSMS, ScopeCategory, string(CategoryMarketing), true)
				in.Preferences = in.Preferences.Override(ChannelSMS, ScopeChannel, "*", true)
				in.Destinations.PhoneVerified = false
				in.RequestedChannels = []Channel{ChannelSMS}
				return in
			}(),
			wantOff: map[Channel]SuppressionReason{ChannelSMS: SuppressNoDestination},
		},
		{
			name: "duplicate domain delivery",
			in: func() IntentInput {
				in := baseInput(EventMessagingMessageReceived)
				in.AlreadySeen = true
				return in
			}(),
			wantOff: map[Channel]SuppressionReason{ChannelInApp: SuppressDeduplicated},
		},
		{
			name: "email marketing does not fall back to sms",
			in: func() IntentInput {
				in := baseInput(EventMarketingCampaign)
				in.Consents = []ConsentSnapshot{{
					Type: ConsentCommercialSMS, Status: ConsentGranted, RecordedSeq: 1,
					CapturedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Source: ConsentSourceSettingsWeb,
				}}
				in.Preferences = in.Preferences.Override(ChannelSMS, ScopeCategory, string(CategoryMarketing), true)
				in.Preferences = in.Preferences.Override(ChannelEmail, ScopeCategory, string(CategoryMarketing), true)
				in.Preferences = in.Preferences.Override(ChannelSMS, ScopeChannel, "*", true)
				in.Preferences = in.Preferences.Override(ChannelEmail, ScopeChannel, "*", true)
				in.RequestedChannels = []Channel{ChannelEmail, ChannelSMS}
				return in
			}(),
			wantOn:  []Channel{ChannelSMS},
			wantOff: map[Channel]SuppressionReason{ChannelEmail: SuppressConsentMissing},
		},
	}

	for _, tc := range run {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.in)
			on := eligibleSet(got)
			for _, ch := range tc.wantOn {
				if !on[ch] {
					t.Fatalf("expected eligible %s decisions=%v", ch, got.Decisions)
				}
			}
			for ch, reason := range tc.wantOff {
				if on[ch] {
					t.Fatalf("%s should be suppressed", ch)
				}
				if suppression(got, ch) != reason {
					t.Fatalf("%s reason=%s want=%s", ch, suppression(got, ch), reason)
				}
			}
		})
	}
}

func TestDedupeSameEventOnce(t *testing.T) {
	a := DedupeKey(EventMessagingMessageReceived, "u1", "msg-1")
	b := DedupeKey(EventMessagingMessageReceived, "u1", "msg-1")
	c := DedupeKey(EventMessagingMessageReceived, "u1", "msg-2")
	if a != b || a == c {
		t.Fatalf("dedupe a=%s b=%s c=%s", a, b, c)
	}
}

func TestAuthSecurityMapping(t *testing.T) {
	if !MapAuthSecurityEvent(AuthPasskeyAdded).Notify || MapAuthSecurityEvent(AuthPasskeyAdded).EventType != EventSecurityPasskeyAdded {
		t.Fatal("passkey added")
	}
	if MapAuthSecurityEvent(AuthLoginFailed).Notify {
		t.Fatal("failed login must not email the user")
	}
	if MapAuthSecurityEvent(AuthRateLimitTriggered).Notify || MapAuthSecurityEvent(AuthChallengeFailed).Notify {
		t.Fatal("abuse events are audit only")
	}
}

func TestTemplateRejectsSecrets(t *testing.T) {
	spec := MustEvent(EventOfferReceived)
	if _, err := FilterVariables(spec, map[string]string{"otp": "123456"}); !errors.Is(err, ErrArbitraryMetadata) {
		t.Fatalf("err=%v", err)
	}
	if _, err := FilterVariables(spec, map[string]string{"email": "a@b.c"}); !errors.Is(err, ErrArbitraryMetadata) {
		t.Fatalf("email var err=%v", err)
	}
	if _, err := FilterVariables(spec, map[string]string{"phone": "+905551112233"}); !errors.Is(err, ErrArbitraryMetadata) {
		t.Fatalf("phone var err=%v", err)
	}
	if _, err := FilterVariables(spec, map[string]string{"need_id": "n1", "offer_id": "o1"}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordedSeqBeatsRecordedAt(t *testing.T) {
	older := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	in := baseInput(EventMarketingCampaign)
	in.RequestedChannels = []Channel{ChannelEmail}
	in.Preferences = in.Preferences.Override(ChannelEmail, ScopeCategory, string(CategoryMarketing), true)
	in.Consents = []ConsentSnapshot{
		{Type: ConsentCommercialEmail, Status: ConsentGranted, RecordedSeq: 1, CapturedAt: older, Source: ConsentSourceSettingsWeb},
		{Type: ConsentCommercialEmail, Status: ConsentWithdrawn, RecordedSeq: 2, CapturedAt: newer, Source: ConsentSourceSettingsWeb, WithdrawnAt: &newer},
	}
	got := Resolve(in)
	if suppression(got, ChannelEmail) != SuppressConsentWithdrawn {
		t.Fatalf("current consent must use max recorded_seq, got %s", suppression(got, ChannelEmail))
	}
}

func TestStoredMuteDoesNotSuppressRequiredChannel(t *testing.T) {
	in := baseInput(EventSecurityPasskeyAdded)
	in.Preferences = in.Preferences.Override(ChannelInApp, ScopeChannel, "*", false)
	in.Preferences = in.Preferences.Override(ChannelEmail, ScopeChannel, "*", false)
	got := Resolve(in)
	on := eligibleSet(got)
	if !on[ChannelInApp] || !on[ChannelEmail] {
		t.Fatalf("required channels must remain eligible: %v", got.Decisions)
	}
}

func TestConsentStoreFailureFailsClosedForMarketing(t *testing.T) {
	in := baseInput(EventMarketingCampaign)
	in.ConsentLookupFailed = true
	in.Preferences = in.Preferences.Override(ChannelEmail, ScopeCategory, string(CategoryMarketing), true)
	in.RequestedChannels = []Channel{ChannelEmail}
	got := Resolve(in)
	if suppression(got, ChannelEmail) != SuppressConsentMissing {
		t.Fatalf("reason=%s", suppression(got, ChannelEmail))
	}
}

func TestPreferenceStoreFailureDoesNotGuessOptional(t *testing.T) {
	in := baseInput(EventMessagingMessageReceived)
	in.PreferenceLookupFailed = true
	got := Resolve(in)
	if eligibleSet(got)[ChannelMobilePush] {
		t.Fatal("optional channel must not be guessed on")
	}
	sec := baseInput(EventSecurityPasskeyAdded)
	sec.PreferenceLookupFailed = true
	gotSec := Resolve(sec)
	on := eligibleSet(gotSec)
	if !on[ChannelInApp] || !on[ChannelEmail] {
		t.Fatalf("security required must ignore preference store failure: %v", gotSec.Decisions)
	}
}

func TestRetryPermanentForConsent(t *testing.T) {
	if ClassifyRetry(SuppressConsentMissing, true) != RetryPermanent {
		t.Fatal("consent missing is permanent")
	}
	delay, giveUp := BoundedBackoff(8)
	if !giveUp || delay != 0 {
		t.Fatalf("backoff %d %v", delay, giveUp)
	}
}
