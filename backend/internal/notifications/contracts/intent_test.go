package contracts

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDecodeIntentAcceptsV1(t *testing.T) {
	raw := mustJSON(t, validIntent())
	got, err := DecodeIntent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.IntentID != validIntent().IntentID || got.Channel != ChannelEmail {
		t.Fatalf("decode mismatch: %+v", got)
	}
	if got.Recipient.Kind != RecipientVerificationChallenge {
		t.Fatalf("recipient = %+v", got.Recipient)
	}
}

func TestIntentValidateRejectsBadFields(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Intent)
	}{
		{"intent_id", func(i *Intent) { i.IntentID = "not-a-uuid" }},
		{"version", func(i *Intent) { i.Version = 2 }},
		{"purpose", func(i *Intent) { i.Purpose = "promo" }},
		{"template", func(i *Intent) { i.TemplateCode = "Bad.Code" }},
		{"channel", func(i *Intent) { i.Channel = "push" }},
		{"locale", func(i *Intent) { i.Locale = "fr" }},
		{"recipient kind", func(i *Intent) { i.Recipient.Kind = "email" }},
		{"recipient id", func(i *Intent) { i.Recipient.ID = "" }},
		{"created_at", func(i *Intent) { i.CreatedAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := validIntent()
			tc.mut(&i)
			if err := i.Validate(); !errors.Is(err, ErrInvalidIntent) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestDecodeIntentRejectsMalformed(t *testing.T) {
	bads := []json.RawMessage{
		nil,
		json.RawMessage(`not-json`),
		json.RawMessage(`[]`),
		json.RawMessage(`null`),
		json.RawMessage(`{"intent_id":"11111111-1111-4111-8111-111111111111"}`),
	}
	for _, raw := range bads {
		if _, err := DecodeIntent(raw); !errors.Is(err, ErrInvalidIntent) {
			t.Fatalf("%s err = %v", raw, err)
		}
	}
}

func TestDecodeIntentRejectsUnknownFields(t *testing.T) {
	raw := json.RawMessage(`{
		"intent_id":"11111111-1111-4111-8111-111111111111",
		"version":1,
		"purpose":"security",
		"template_code":"identity.verification.signup",
		"channel":"email",
		"locale":"tr",
		"recipient":{"kind":"verification_challenge","id":"22222222-2222-4222-8222-222222222222"},
		"created_at":"2026-09-06T12:00:00Z",
		"extra":"nope"
	}`)
	if _, err := DecodeIntent(raw); !errors.Is(err, ErrInvalidIntent) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeIntentRejectsSecretFields(t *testing.T) {
	secrets := []string{
		`{"otp":"123456"}`,
		`{"phone_otp":"123456"}`,
		`{"email_token":"abc"}`,
		`{"token":"abc"}`,
		`{"password":"x"}`,
		`{"session_token":"x"}`,
		`{"ceremony_token":"x"}`,
		`{"raw_secret":"x"}`,
		`{"recipient":{"kind":"verification_challenge","id":"22222222-2222-4222-8222-222222222222","otp":"1"}}`,
	}
	base := validIntent()
	for _, extra := range secrets {
		var overlay map[string]any
		if err := json.Unmarshal([]byte(extra), &overlay); err != nil {
			t.Fatal(err)
		}
		raw := mergeJSON(t, base, overlay)
		_, err := DecodeIntent(raw)
		if !errors.Is(err, ErrSensitivePayload) {
			t.Fatalf("%s err = %v, want %v", extra, err, ErrSensitivePayload)
		}
	}
}

func TestVerificationIntentMayReferenceChallengeID(t *testing.T) {
	i := validIntent()
	i.Recipient = RecipientRef{Kind: RecipientVerificationChallenge, ID: "33333333-3333-4333-8333-333333333333"}
	raw, err := json.Marshal(i)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeIntent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Recipient.ID != i.Recipient.ID {
		t.Fatalf("challenge ref lost: %s", got.Recipient.ID)
	}
}

func validIntent() Intent {
	return Intent{
		IntentID:     "11111111-1111-4111-8111-111111111111",
		Version:      IntentEventVersion,
		Purpose:      PurposeSecurity,
		TemplateCode: TemplateIdentityVerificationSignup,
		Channel:      ChannelEmail,
		Locale:       LocaleTR,
		Recipient: RecipientRef{
			Kind: RecipientVerificationChallenge,
			ID:   "22222222-2222-4222-8222-222222222222",
		},
		CorrelationID: "corr-1",
		CreatedAt:     time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}
}

func mustJSON(t *testing.T, i Intent) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(i)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mergeJSON(t *testing.T, base Intent, overlay map[string]any) json.RawMessage {
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
		if k == "recipient" {
			if rec, ok := v.(map[string]any); ok {
				existing, _ := m["recipient"].(map[string]any)
				if existing == nil {
					existing = map[string]any{}
				}
				for rk, rv := range rec {
					existing[rk] = rv
				}
				m["recipient"] = existing
				continue
			}
		}
		m[k] = v
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
