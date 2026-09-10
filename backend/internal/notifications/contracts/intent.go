package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
)

// V1 outbox routing for a notification intent. Producers enqueue this type.
const (
	IntentEventType    = "notifications.intent"
	IntentEventVersion = 1
)

// V1 template codes. Catalog expansion is owned by Notifications.
const (
	TemplateIdentityVerificationSignup = "identity.verification.signup"
	TemplateIdentityPasswordReset      = "identity.password.reset"
)

const (
	ChannelEmail Channel = "email"
	ChannelSMS   Channel = "sms"
)

const (
	PurposeSecurity      Purpose = "security"
	PurposeTransactional Purpose = "transactional"
	PurposeMarketing     Purpose = "marketing"
)

const (
	RecipientVerificationChallenge RecipientKind = "verification_challenge"
	RecipientUser                  RecipientKind = "user"
)

const (
	LocaleTR = "tr"
	LocaleEN = "en"
	LocaleRU = "ru"
	LocaleAR = "ar"
)

var (
	ErrInvalidIntent    = errors.New("invalid notification intent")
	ErrSensitivePayload = errors.New("notification intent must not contain secrets")
)

type Channel string
type Purpose string
type RecipientKind string

// RecipientRef identifies who/where to deliver without contact or secret material.
type RecipientRef struct {
	Kind RecipientKind `json:"kind"`
	ID   string        `json:"id"`
}

// Intent is the stable V1 cross-domain notification intent.
// It is the outbox payload producers write. It must not contain OTP, email
// token, password, session token, ceremony token, or other credential material.
type Intent struct {
	IntentID      string       `json:"intent_id"`
	Version       int          `json:"version"`
	Purpose       Purpose      `json:"purpose"`
	TemplateCode  string       `json:"template_code"`
	Channel       Channel      `json:"channel"`
	Locale        string       `json:"locale"`
	Recipient     RecipientRef `json:"recipient"`
	CorrelationID string       `json:"correlation_id,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
}

func (i Intent) Validate() error {
	if !uuidString(i.IntentID) {
		return ErrInvalidIntent
	}
	if i.Version != IntentEventVersion {
		return ErrInvalidIntent
	}
	if !i.Purpose.valid() {
		return ErrInvalidIntent
	}
	if !templateCode(i.TemplateCode) {
		return ErrInvalidIntent
	}
	if !i.Channel.valid() {
		return ErrInvalidIntent
	}
	if !locale(i.Locale) {
		return ErrInvalidIntent
	}
	if err := i.Recipient.Validate(); err != nil {
		return err
	}
	if i.CorrelationID != strings.TrimSpace(i.CorrelationID) {
		return ErrInvalidIntent
	}
	if i.CreatedAt.IsZero() {
		return ErrInvalidIntent
	}
	return nil
}

func (r RecipientRef) Validate() error {
	if !r.Kind.valid() {
		return ErrInvalidIntent
	}
	if !uuidString(r.ID) {
		return ErrInvalidIntent
	}
	return nil
}

func (c Channel) valid() bool {
	return c == ChannelEmail || c == ChannelSMS
}

func (p Purpose) valid() bool {
	return p == PurposeSecurity || p == PurposeTransactional || p == PurposeMarketing
}

func (k RecipientKind) valid() bool {
	return k == RecipientVerificationChallenge || k == RecipientUser
}

func ValidLocale(s string) bool {
	return locale(s)
}

func ValidTemplateCode(s string) bool {
	return templateCode(s)
}

func locale(s string) bool {
	switch s {
	case LocaleTR, LocaleEN, LocaleRU, LocaleAR:
		return true
	default:
		return false
	}
}

func templateCode(s string) bool {
	if s != strings.TrimSpace(s) || s == "" {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if unicode.IsUpper(r) || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
				return false
			}
		}
	}
	return true
}

func uuidString(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !isHex(r) {
				return false
			}
		}
	}
	return true
}

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// DecodeIntent validates V1 JSON. Unknown fields and secret keys are rejected.
func DecodeIntent(raw json.RawMessage) (Intent, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return Intent{}, ErrInvalidIntent
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return Intent{}, ErrInvalidIntent
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return Intent{}, ErrInvalidIntent
	}
	if err := rejectSensitive(obj); err != nil {
		return Intent{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var intent Intent
	if err := dec.Decode(&intent); err != nil {
		return Intent{}, ErrInvalidIntent
	}
	if err := intent.Validate(); err != nil {
		return Intent{}, err
	}
	return intent, nil
}

func rejectSensitive(v any) error {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if sensitiveKey(k) {
				return ErrSensitivePayload
			}
			if err := rejectSensitive(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range t {
			if err := rejectSensitive(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func sensitiveKey(k string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(k) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	n := b.String()
	switch n {
	case "password", "passwd", "otp", "token", "secret", "cookie", "authorization",
		"sessiontoken", "accesstoken", "refreshtoken", "apikey", "privatekey",
		"rawtoken", "rawsecret", "rawotp":
		return true
	}
	for _, frag := range []string{"otp", "token", "password", "passwd", "secret", "cookie", "credential", "apikey", "privatekey"} {
		if strings.Contains(n, frag) {
			return true
		}
	}
	return false
}
