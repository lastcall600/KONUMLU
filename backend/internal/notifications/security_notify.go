package notifications

import (
	"context"
	"encoding/json"
	"strings"

	"backend/internal/notifications/policy"
	"backend/internal/platform/observability"
	"backend/internal/platform/outbox"
)

const authSecurityOutboxType = "identity.auth.security"
const authSecurityOutboxVersion = 1

// NewAuthSecurityNotifyHandler consumes identity.auth.security v1 after the
// Identity audit logger. Transient materialization errors must not complete the
// outbox row.
func NewAuthSecurityNotifyHandler(m *Materializer) outbox.Handler {
	return outbox.HandlerFunc(func(ctx context.Context, event outbox.Event) error {
		if event.EventType != authSecurityOutboxType || event.EventVersion != authSecurityOutboxVersion {
			return errInvalidEvent
		}
		var payload struct {
			Event     string `json:"event"`
			UserID    string `json:"user_id"`
			SessionID string `json:"session_id"`
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			observability.FromContext(ctx).Info("notification_auth_security_invalid",
				"event_type", event.EventType,
				"error_class", "invalid_event",
			)
			return nil
		}
		mapped := policy.MapAuthSecurityEvent(payload.Event)
		if !mapped.Notify {
			observability.FromContext(ctx).Info("notification_auth_security_audit_only",
				"event_type", payload.Event,
				"suppression_reason", mapped.Reason,
			)
			return nil
		}
		userID, err := ParseID(strings.TrimSpace(payload.UserID))
		if err != nil || userID.IsZero() {
			observability.FromContext(ctx).Info("notification_auth_security_no_user",
				"event_type", payload.Event,
			)
			return nil
		}
		if m == nil {
			return errStoreRequired
		}
		createdAt := event.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
		_, err = m.Materialize(ctx, MaterializeInput{
			RecipientUserID: userID,
			EventType:       mapped.EventType,
			DomainRef:       event.ID.String(),
			Variables:       map[string]string{"created_at": createdAt},
		})
		return err
	})
}
