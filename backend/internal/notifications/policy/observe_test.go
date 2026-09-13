package policy

import (
	"testing"
	"time"

	"backend/internal/platform/observability"
)

func TestSafeIntentLogFieldsDoNotIncludeDestinations(t *testing.T) {
	fields := map[string]string{
		"notify_purpose":     string(PurposeSecurity),
		"notification_event": string(EventSecurityPasskeyAdded),
		"suppression_reason": string(SuppressConsentMissing),
		"delivery_outcome":   string(DeliverySuppressed),
		"channel_code":       string(ChannelEmail),
		"email":              "owner@example.test",
		"push_token":         "ExponentPushToken[abc]",
		"consent_payload":    `{"ip":"1.2.3.4"}`,
	}
	for k, v := range fields {
		got := observability.RedactAttrValue(k, v)
		switch k {
		case "email", "push_token", "consent_payload":
			if got != observability.Redacted {
				t.Fatalf("%s leaked as %q", k, got)
			}
		default:
			if got != v {
				t.Fatalf("%s redacted unexpectedly: %q", k, got)
			}
		}
	}
}

func TestExistingOutboxIntentStillRejectsSecrets(t *testing.T) {
	now := time.Now().UTC()
	_ = now
	spec := MustEvent(EventIdentityPasswordReset)
	if _, err := FilterVariables(spec, map[string]string{"password": "x"}); err == nil {
		t.Fatal("password variable")
	}
}
