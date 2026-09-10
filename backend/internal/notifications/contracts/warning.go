package contracts

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// V1 outbox routing for a moderation warning. Moderation produces this type;
// Notifications owns delivery intent and delivery state.
const (
	WarningEventType    = "notifications.moderation.warning"
	WarningEventVersion = 1

	TemplateModerationWarningIssued = "moderation.warning.issued"

	MessageKeyWarningListing       = "moderation.warning.listing"
	MessageKeyWarningPublicProfile = "moderation.warning.public_profile"

	WarningTargetListing       = "listing"
	WarningTargetPublicProfile = "public_profile"

	ChannelInApp Channel = "in_app"
)

const (
	WarningReasonNoViolation       = "no_violation"
	WarningReasonPolicyViolation   = "policy_violation"
	WarningReasonRepeatedViolation = "repeated_violation"
	WarningReasonSafetyRisk        = "safety_risk"
	WarningReasonProhibitedContent = "prohibited_content"
	WarningReasonOther             = "other"
)

// WarningUserPayload is the only client-safe view of a warning.
// It must not include recipient identity, reporter, staff, rationale, or evidence.
type WarningUserPayload struct {
	MessageKey   string    `json:"message_key"`
	Locale       string    `json:"locale"`
	TemplateCode string    `json:"template_code"`
	TargetType   string    `json:"target_type"`
	TargetRef    string    `json:"target_ref"`
	ReasonCode   string    `json:"reason_code"`
	ActionAt     time.Time `json:"action_at"`
}

// WarningIntent is the durable outbox payload. Recipient is routing-only
// (Notifications delivery). Call UserPayload() for anything user-facing.
type WarningIntent struct {
	IntentID      string       `json:"intent_id"`
	Version       int          `json:"version"`
	Purpose       Purpose      `json:"purpose"`
	TemplateCode  string       `json:"template_code"`
	Channel       Channel      `json:"channel"`
	Locale        string       `json:"locale"`
	Recipient     RecipientRef `json:"recipient"`
	MessageKey    string       `json:"message_key"`
	TargetType    string       `json:"target_type"`
	TargetRef     string       `json:"target_ref"`
	ReasonCode    string       `json:"reason_code"`
	ActionAt      time.Time    `json:"action_at"`
	CorrelationID string       `json:"correlation_id,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
}

func (w WarningIntent) UserPayload() WarningUserPayload {
	return WarningUserPayload{
		MessageKey:   w.MessageKey,
		Locale:       w.Locale,
		TemplateCode: w.TemplateCode,
		TargetType:   w.TargetType,
		TargetRef:    w.TargetRef,
		ReasonCode:   w.ReasonCode,
		ActionAt:     w.ActionAt.UTC(),
	}
}

func (w WarningIntent) Validate() error {
	if !uuidString(w.IntentID) {
		return ErrInvalidIntent
	}
	if w.Version != WarningEventVersion {
		return ErrInvalidIntent
	}
	if w.Purpose != PurposeTransactional {
		return ErrInvalidIntent
	}
	if w.TemplateCode != TemplateModerationWarningIssued || !templateCode(w.TemplateCode) {
		return ErrInvalidIntent
	}
	if w.Channel != ChannelInApp {
		return ErrInvalidIntent
	}
	if !locale(w.Locale) {
		return ErrInvalidIntent
	}
	if err := w.Recipient.Validate(); err != nil {
		return err
	}
	if w.Recipient.Kind != RecipientUser {
		return ErrInvalidIntent
	}
	if !validWarningMessageKey(w.MessageKey) {
		return ErrInvalidIntent
	}
	if !validWarningTargetType(w.TargetType) {
		return ErrInvalidIntent
	}
	if !uuidString(w.TargetRef) {
		return ErrInvalidIntent
	}
	if !validWarningReason(w.ReasonCode) {
		return ErrInvalidIntent
	}
	if w.ActionAt.IsZero() || w.CreatedAt.IsZero() {
		return ErrInvalidIntent
	}
	if w.CorrelationID != strings.TrimSpace(w.CorrelationID) {
		return ErrInvalidIntent
	}
	if w.MessageKey == MessageKeyWarningListing && w.TargetType != WarningTargetListing {
		return ErrInvalidIntent
	}
	if w.MessageKey == MessageKeyWarningPublicProfile && w.TargetType != WarningTargetPublicProfile {
		return ErrInvalidIntent
	}
	return nil
}

func DecodeWarningIntent(raw json.RawMessage) (WarningIntent, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return WarningIntent{}, ErrInvalidIntent
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return WarningIntent{}, ErrInvalidIntent
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return WarningIntent{}, ErrInvalidIntent
	}
	if err := rejectSensitive(obj); err != nil {
		return WarningIntent{}, err
	}
	if err := rejectWarningLeak(obj); err != nil {
		return WarningIntent{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var intent WarningIntent
	if err := dec.Decode(&intent); err != nil {
		return WarningIntent{}, ErrInvalidIntent
	}
	if err := intent.Validate(); err != nil {
		return WarningIntent{}, err
	}
	return intent, nil
}

func ValidDeliveryChannel(c Channel) bool {
	return c == ChannelEmail || c == ChannelSMS || c == ChannelInApp
}

func validWarningMessageKey(s string) bool {
	return s == MessageKeyWarningListing || s == MessageKeyWarningPublicProfile
}

func validWarningTargetType(s string) bool {
	return s == WarningTargetListing || s == WarningTargetPublicProfile
}

func validWarningReason(s string) bool {
	switch s {
	case WarningReasonNoViolation, WarningReasonPolicyViolation, WarningReasonRepeatedViolation,
		WarningReasonSafetyRisk, WarningReasonProhibitedContent, WarningReasonOther:
		return true
	default:
		return false
	}
}

func rejectWarningLeak(v any) error {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if warningLeakKey(k) {
				return ErrSensitivePayload
			}
			if err := rejectWarningLeak(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range t {
			if err := rejectWarningLeak(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func warningLeakKey(k string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(k) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	n := b.String()
	switch n {
	case "reporter", "reporterid", "reporteruserid", "staff", "staffid", "staffnote",
		"actorstaffid", "rationale", "evidence", "note", "userid", "session",
		"casenote", "internalnote":
		return true
	}
	for _, frag := range []string{"reporter", "staffnote", "rationale", "evidence", "actorstaff"} {
		if strings.Contains(n, frag) {
			return true
		}
	}
	return false
}
