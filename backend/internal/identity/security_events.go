package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/platform/outbox"
)

const (
	// AuthSecurityEventType is the platform outbox type for Identity auth audit.
	AuthSecurityEventType    = "identity.auth.security"
	AuthSecurityEventVersion = 1
	authSecurityAggregate    = "identity_auth"
)

// AuthSecurityEvent is a controlled auth audit name. Clients cannot submit these.
type AuthSecurityEvent string

const (
	AuthEventLoginSuccess           AuthSecurityEvent = "auth.login.success"
	AuthEventLoginFailed            AuthSecurityEvent = "auth.login.failed"
	AuthEventLogout                 AuthSecurityEvent = "auth.logout"
	AuthEventSessionRevoked         AuthSecurityEvent = "auth.session.revoked"
	AuthEventSessionsRevokedOthers  AuthSecurityEvent = "auth.sessions.revoked_others"
	AuthEventPasswordResetCompleted AuthSecurityEvent = "auth.password_reset.completed"
	AuthEventPasskeyAdded           AuthSecurityEvent = "auth.passkey.added"
	AuthEventPasskeyRemoved         AuthSecurityEvent = "auth.passkey.removed"
	AuthEventStepUpSuccess          AuthSecurityEvent = "auth.step_up.success"
	AuthEventStepUpFailed           AuthSecurityEvent = "auth.step_up.failed"
	AuthEventRateLimitTriggered     AuthSecurityEvent = "auth.rate_limit.triggered"
	AuthEventChallengeRequired      AuthSecurityEvent = "auth.challenge.required"
	AuthEventChallengeFailed        AuthSecurityEvent = "auth.challenge.failed"
)

func (e AuthSecurityEvent) valid() bool {
	switch e {
	case AuthEventLoginSuccess, AuthEventLoginFailed, AuthEventLogout,
		AuthEventSessionRevoked, AuthEventSessionsRevokedOthers,
		AuthEventPasswordResetCompleted, AuthEventPasskeyAdded, AuthEventPasskeyRemoved,
		AuthEventStepUpSuccess, AuthEventStepUpFailed, AuthEventRateLimitTriggered,
		AuthEventChallengeRequired, AuthEventChallengeFailed:
		return true
	default:
		return false
	}
}

// StateChanging reports events that must be durable with the domain mutation.
// Observational/attempt events may be best-effort.
func (e AuthSecurityEvent) StateChanging() bool {
	switch e {
	case AuthEventLoginSuccess, AuthEventLogout,
		AuthEventSessionRevoked, AuthEventSessionsRevokedOthers,
		AuthEventPasswordResetCompleted, AuthEventPasskeyAdded, AuthEventPasskeyRemoved,
		AuthEventStepUpSuccess:
		return true
	default:
		return false
	}
}

const (
	AuthMethodPassword = "password"
	AuthMethodPasskey  = "passkey"
	AuthMethodSignup   = "signup"

	AuthResultSuccess   = "success"
	AuthResultFailed    = "failed"
	AuthResultTriggered = "triggered"
	AuthResultRequired  = "required"
)

// SecurityRecord is safe operational metadata for one auth security event.
// It must not contain passwords, OTP, email/phone, cookies, CSRF, proofs,
// challenge tokens, passkey material, TCKN, or provider secrets.
type SecurityRecord struct {
	Type         AuthSecurityEvent
	UserID       ID
	SessionID    ID
	AuthMethod   string
	Operation    AuthOperation
	Result       string
	RiskDecision RiskDecision
	ReasonCode   RiskReason
	RequestID    string
	TraceID      string
	Provider     string
	ErrorClass   string
	DedupeKey    string
}

// SecurityRecorder persists a controlled auth security event.
type SecurityRecorder interface {
	Record(ctx context.Context, rec SecurityRecord) error
}

// TxSecurityRecorder writes the event on a caller-owned PostgreSQL transaction.
type TxSecurityRecorder interface {
	SecurityRecorder
	RecordOn(ctx context.Context, exec outbox.Execer, rec SecurityRecord) error
}

// MemorySecurityRecorder is an in-process sink for tests. It is not production audit.
type MemorySecurityRecorder struct {
	mu      sync.Mutex
	Records []SecurityRecord
}

func (m *MemorySecurityRecorder) Record(_ context.Context, rec SecurityRecord) error {
	if m == nil {
		return errStoreRequired
	}
	if err := rec.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Records = append(m.Records, rec)
	return nil
}

func (m *MemorySecurityRecorder) RecordOn(ctx context.Context, _ outbox.Execer, rec SecurityRecord) error {
	return m.Record(ctx, rec)
}

func (m *MemorySecurityRecorder) Snapshot() []SecurityRecord {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SecurityRecord, len(m.Records))
	copy(out, m.Records)
	return out
}

func (m *MemorySecurityRecorder) Types() []AuthSecurityEvent {
	recs := m.Snapshot()
	out := make([]AuthSecurityEvent, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec.Type)
	}
	return out
}

type securityEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

// OutboxSecurityRecorder writes identity.auth.security v1 into platform.outbox_events.
// No schema migration is required. High-volume abuse events use idempotency so
// a stuffing spike cannot flood the outbox.
type OutboxSecurityRecorder struct {
	out securityEnqueuer
	now func() time.Time
}

func NewOutboxSecurityRecorder(out securityEnqueuer, now func() time.Time) (*OutboxSecurityRecorder, error) {
	if out == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &OutboxSecurityRecorder{out: out, now: now}, nil
}

func (r *OutboxSecurityRecorder) Record(ctx context.Context, rec SecurityRecord) error {
	return r.RecordOn(ctx, nil, rec)
}

// RecordOn writes the event on exec (caller transaction) or the outbox pool when exec is nil.
func (r *OutboxSecurityRecorder) RecordOn(ctx context.Context, exec outbox.Execer, rec SecurityRecord) error {
	if r == nil || r.out == nil {
		return errStoreRequired
	}
	return enqueueAuthSecurity(ctx, r.out, exec, rec, r.now())
}

func enqueueAuthSecurity(ctx context.Context, out securityEnqueuer, exec outbox.Execer, rec SecurityRecord, now time.Time) error {
	if out == nil {
		return errStoreRequired
	}
	if err := rec.validate(); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	payload, err := encodeAuthSecurityPayload(rec, now)
	if err != nil {
		return err
	}
	in := outbox.NewEvent{
		EventType:      AuthSecurityEventType,
		EventVersion:   AuthSecurityEventVersion,
		AggregateType:  authSecurityAggregate,
		Payload:        payload,
		IdempotencyKey: rec.idempotencyKey(now),
		CorrelationID:  strings.TrimSpace(rec.RequestID),
	}
	if !rec.UserID.IsZero() {
		in.AggregateID = rec.UserID.String()
	} else if !rec.SessionID.IsZero() {
		in.AggregateID = rec.SessionID.String()
	}
	_, err = out.Enqueue(ctx, exec, in)
	if err == nil {
		return nil
	}
	if errors.Is(err, outbox.ErrConflict) {
		return nil
	}
	return mapSecurityErr(err)
}

func (rec SecurityRecord) validate() error {
	if !rec.Type.valid() {
		return errInvalidAbusePolicy
	}
	if !validAuthResult(rec.Result) {
		return errInvalidAbusePolicy
	}
	if rec.AuthMethod != "" && !validAuthMethod(rec.AuthMethod) {
		return errInvalidAbusePolicy
	}
	if rec.Operation != "" && !rec.Operation.valid() {
		return errInvalidAbusePolicy
	}
	if rec.RiskDecision != "" && !rec.RiskDecision.valid() {
		return errInvalidAbusePolicy
	}
	if rec.ReasonCode != "" && !rec.ReasonCode.valid() {
		return errInvalidAbusePolicy
	}
	return nil
}

func (rec SecurityRecord) idempotencyKey(now time.Time) string {
	if key := strings.TrimSpace(rec.DedupeKey); key != "" {
		return "identity.auth.security:" + HashRateLimitSubject(key)
	}
	switch rec.Type {
	case AuthEventRateLimitTriggered, AuthEventChallengeRequired, AuthEventChallengeFailed:
		window := now.UTC().Unix() / 60
		raw := string(rec.Type) + "|" + string(rec.Operation) + "|" + string(rec.ReasonCode) + "|" + rec.ErrorClass + "|" + rec.RequestID
		return "identity.auth.security:" + HashRateLimitSubject(raw) + ":" + strconv.FormatInt(window, 10)
	default:
		return ""
	}
}

type authSecurityPayload struct {
	Event        string `json:"event"`
	UserID       string `json:"user_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	AuthMethod   string `json:"auth_method,omitempty"`
	Operation    string `json:"operation,omitempty"`
	Result       string `json:"result"`
	RiskDecision string `json:"risk_decision,omitempty"`
	ReasonCode   string `json:"reason_code,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
	TraceID      string `json:"trace_id,omitempty"`
	OccurredAt   string `json:"occurred_at"`
	Provider     string `json:"provider,omitempty"`
	ErrorClass   string `json:"error_class,omitempty"`
}

func encodeAuthSecurityPayload(rec SecurityRecord, now time.Time) (json.RawMessage, error) {
	p := authSecurityPayload{
		Event:        string(rec.Type),
		AuthMethod:   strings.TrimSpace(rec.AuthMethod),
		Operation:    string(rec.Operation),
		Result:       rec.Result,
		RiskDecision: string(rec.RiskDecision),
		ReasonCode:   string(rec.ReasonCode),
		RequestID:    strings.TrimSpace(rec.RequestID),
		TraceID:      strings.TrimSpace(rec.TraceID),
		OccurredAt:   now.UTC().Format(time.RFC3339),
		Provider:     strings.TrimSpace(rec.Provider),
		ErrorClass:   strings.TrimSpace(rec.ErrorClass),
	}
	if !rec.UserID.IsZero() {
		p.UserID = rec.UserID.String()
	}
	if !rec.SessionID.IsZero() {
		p.SessionID = rec.SessionID.String()
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, errUnavailable
	}
	return raw, nil
}

func decodeAuthSecurityPayload(raw json.RawMessage) (authSecurityPayload, error) {
	var p authSecurityPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return authSecurityPayload{}, errInvalidAbusePolicy
	}
	if !AuthSecurityEvent(p.Event).valid() {
		return authSecurityPayload{}, errInvalidAbusePolicy
	}
	if !validAuthResult(p.Result) {
		return authSecurityPayload{}, errInvalidAbusePolicy
	}
	if p.AuthMethod != "" && !validAuthMethod(p.AuthMethod) {
		return authSecurityPayload{}, errInvalidAbusePolicy
	}
	return p, nil
}

func validAuthMethod(v string) bool {
	switch v {
	case AuthMethodPassword, AuthMethodPasskey, AuthMethodSignup:
		return true
	default:
		return false
	}
}

func validAuthResult(v string) bool {
	switch v {
	case AuthResultSuccess, AuthResultFailed, AuthResultTriggered, AuthResultRequired:
		return true
	default:
		return false
	}
}

func mapSecurityErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, outbox.ErrSensitivePayload) || errors.Is(err, outbox.ErrInvalidEvent) {
		return errInvalidAbusePolicy
	}
	return errUnavailable
}

