package contracts

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecodeWarningIntentAcceptsV1(t *testing.T) {
	raw := mustWarningJSON(t, validWarning())
	got, err := DecodeWarningIntent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Channel != ChannelInApp || got.Recipient.Kind != RecipientUser {
		t.Fatalf("decode mismatch: %+v", got)
	}
	user := got.UserPayload()
	body, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if strings.Contains(s, "recipient") || strings.Contains(s, "reporter") ||
		strings.Contains(s, "staff") || strings.Contains(s, "rationale") ||
		strings.Contains(s, validWarning().Recipient.ID) {
		t.Fatalf("user payload leaked routing or internal data: %s", s)
	}
	if user.TargetRef != validWarning().TargetRef || user.ReasonCode != WarningReasonPolicyViolation {
		t.Fatalf("user payload = %+v", user)
	}
}

func TestWarningIntentValidateRejectsBadFields(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*WarningIntent)
	}{
		{"intent_id", func(w *WarningIntent) { w.IntentID = "not-a-uuid" }},
		{"version", func(w *WarningIntent) { w.Version = 2 }},
		{"purpose", func(w *WarningIntent) { w.Purpose = PurposeSecurity }},
		{"template", func(w *WarningIntent) { w.TemplateCode = "identity.verification.signup" }},
		{"channel", func(w *WarningIntent) { w.Channel = ChannelEmail }},
		{"locale", func(w *WarningIntent) { w.Locale = "fr" }},
		{"recipient kind", func(w *WarningIntent) { w.Recipient.Kind = RecipientVerificationChallenge }},
		{"message key", func(w *WarningIntent) { w.MessageKey = "hello.world" }},
		{"target mismatch", func(w *WarningIntent) { w.TargetType = WarningTargetPublicProfile }},
		{"reason", func(w *WarningIntent) { w.ReasonCode = "staff_judgment" }},
		{"target ref", func(w *WarningIntent) { w.TargetRef = "listing-1" }},
		{"action_at", func(w *WarningIntent) { w.ActionAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := validWarning()
			tc.mut(&w)
			if err := w.Validate(); !errors.Is(err, ErrInvalidIntent) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestDecodeWarningIntentRejectsLeaksAndUnknown(t *testing.T) {
	leaks := []string{
		`{"reporter_user_id":"11111111-1111-4111-8111-111111111111"}`,
		`{"staff_note":"internal"}`,
		`{"rationale":"why"}`,
		`{"evidence":"x"}`,
		`{"user_id":"11111111-1111-4111-8111-111111111111"}`,
		`{"otp":"123456"}`,
	}
	base := validWarning()
	for _, extra := range leaks {
		var overlay map[string]any
		if err := json.Unmarshal([]byte(extra), &overlay); err != nil {
			t.Fatal(err)
		}
		raw := mergeWarningJSON(t, base, overlay)
		_, err := DecodeWarningIntent(raw)
		if !errors.Is(err, ErrSensitivePayload) && !errors.Is(err, ErrInvalidIntent) {
			t.Fatalf("%s err = %v", extra, err)
		}
	}
	raw := mustWarningJSON(t, base)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["extra"] = "nope"
	bad, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWarningIntent(bad); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("unknown field err = %v", err)
	}
}

func TestValidDeliveryChannelIncludesInApp(t *testing.T) {
	if !ValidDeliveryChannel(ChannelInApp) || !ValidDeliveryChannel(ChannelEmail) {
		t.Fatal("expected in_app and email")
	}
	if ValidDeliveryChannel("push") {
		t.Fatal("push is not a V1 delivery channel")
	}
}

func validWarning() WarningIntent {
	return WarningIntent{
		IntentID:     "11111111-1111-4111-8111-111111111111",
		Version:      WarningEventVersion,
		Purpose:      PurposeTransactional,
		TemplateCode: TemplateModerationWarningIssued,
		Channel:      ChannelInApp,
		Locale:       LocaleTR,
		Recipient: RecipientRef{
			Kind: RecipientUser,
			ID:   "22222222-2222-4222-8222-222222222222",
		},
		MessageKey: MessageKeyWarningListing,
		TargetType: WarningTargetListing,
		TargetRef:  "33333333-3333-4333-8333-333333333333",
		ReasonCode: WarningReasonPolicyViolation,
		ActionAt:   time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
	}
}

func mustWarningJSON(t *testing.T, w WarningIntent) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mergeWarningJSON(t *testing.T, base WarningIntent, overlay map[string]any) json.RawMessage {
	t.Helper()
	var m map[string]any
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for k, v := range overlay {
		m[k] = v
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
