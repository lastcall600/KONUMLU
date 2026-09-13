package httpapi

import (
	"net/http"
	"strings"

	"backend/internal/identity"
	"backend/internal/platform/httpx"
	"backend/internal/platform/observability"
)

// SetSecurityRecorder attaches the AUTH-C outbox/log audit sink.
// A nil recorder disables durable events (tests).
func (h *Handler) SetSecurityRecorder(r identity.SecurityRecorder) {
	if h == nil {
		return
	}
	h.events = r
}

func (h *Handler) fillSecurity(r *http.Request, rec identity.SecurityRecord) identity.SecurityRecord {
	if r == nil {
		return rec
	}
	if strings.TrimSpace(rec.RequestID) == "" {
		rec.RequestID = httpx.RequestID(r.Context())
	}
	if strings.TrimSpace(rec.TraceID) == "" {
		rec.TraceID = observability.TraceID(r.Context())
	}
	return rec
}

func (h *Handler) recordSecurity(r *http.Request, rec identity.SecurityRecord) {
	_ = h.writeSecurity(r, rec)
}

func (h *Handler) recordStateChanging(w http.ResponseWriter, r *http.Request, rec identity.SecurityRecord) bool {
	if err := h.writeSecurity(r, rec); err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return false
	}
	return true
}

func (h *Handler) writeSecurity(r *http.Request, rec identity.SecurityRecord) error {
	if h == nil || h.events == nil || r == nil {
		return nil
	}
	rec = h.fillSecurity(r, rec)
	if err := h.events.Record(r.Context(), rec); err != nil {
		observability.FromContext(r.Context()).Info("auth_security_event_write_failed",
			"event_type", string(rec.Type),
			"error_class", "unavailable",
		)
		if rec.Type.StateChanging() {
			return err
		}
	}
	return nil
}

func (h *Handler) recordRisk(r *http.Request, sub identity.AbuseSubject, out identity.RiskOutcome) {
	if out.Allow() {
		return
	}
	rec := identity.SecurityRecord{
		Operation:    out.Operation,
		RiskDecision: out.Decision,
		ReasonCode:   out.Reason,
		Provider:     out.Provider,
		ErrorClass:   riskErrorClass(out),
		UserID:       sub.AccountID,
		SessionID:    sub.SessionID,
	}
	switch out.Reason {
	case identity.ReasonVelocityIP, identity.ReasonVelocityAccount, identity.ReasonVelocityTarget:
		rec.Type = identity.AuthEventRateLimitTriggered
		rec.Result = identity.AuthResultTriggered
		rec.DedupeKey = string(out.Operation) + "|" + string(out.Dimension) + "|" + string(out.Reason)
	case identity.ReasonChallengeRequired:
		rec.Type = identity.AuthEventChallengeRequired
		rec.Result = identity.AuthResultRequired
		rec.DedupeKey = string(out.Operation) + "|challenge_required"
	default:
		rec.Type = identity.AuthEventChallengeFailed
		rec.Result = identity.AuthResultFailed
		rec.DedupeKey = string(out.Operation) + "|" + string(out.Reason)
	}
	h.recordSecurity(r, rec)
}
