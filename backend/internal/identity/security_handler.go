package identity

import (
	"context"

	"backend/internal/platform/observability"
	"backend/internal/platform/outbox"
)

// NewAuthSecurityHandler is the worker sink for identity.auth.security v1.
// It logs allowlisted operational fields and completes the outbox row.
// It does not write Trust, send notifications, or treat events as a risk authority.
func NewAuthSecurityHandler() outbox.Handler {
	return outbox.HandlerFunc(func(ctx context.Context, event outbox.Event) error {
		if event.EventType != AuthSecurityEventType || event.EventVersion != AuthSecurityEventVersion {
			return errInvalidAbusePolicy
		}
		p, err := decodeAuthSecurityPayload(event.Payload)
		if err != nil {
			observability.FromContext(ctx).Info("auth_security_event_invalid",
				"event_type", event.EventType,
				"event_version", event.EventVersion,
				"error_class", "invalid_event",
			)
			return nil
		}
		observability.FromContext(ctx).Info("auth_security_event",
			"event_type", p.Event,
			"auth_method", p.AuthMethod,
			"result", p.Result,
			"reason_code", p.ReasonCode,
			"risk_decision", p.RiskDecision,
			"auth_operation", p.Operation,
			"request_id", p.RequestID,
			"trace_id", p.TraceID,
			"provider", p.Provider,
			"error_class", p.ErrorClass,
			"user_id", p.UserID,
			"session_id", p.SessionID,
		)
		return nil
	})
}
