package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/platform/outbox"
)

func TestAuthSecurityEventStateChangingClassification(t *testing.T) {
	changing := []AuthSecurityEvent{
		AuthEventLoginSuccess, AuthEventLogout,
		AuthEventSessionRevoked, AuthEventSessionsRevokedOthers,
		AuthEventPasswordResetCompleted, AuthEventPasskeyAdded, AuthEventPasskeyRemoved,
		AuthEventStepUpSuccess,
	}
	for _, e := range changing {
		if !e.StateChanging() {
			t.Fatalf("%s must be state-changing", e)
		}
	}
	observational := []AuthSecurityEvent{
		AuthEventLoginFailed, AuthEventStepUpFailed,
		AuthEventRateLimitTriggered, AuthEventChallengeRequired, AuthEventChallengeFailed,
	}
	for _, e := range observational {
		if e.StateChanging() {
			t.Fatalf("%s must remain observational", e)
		}
	}
}

func TestAuthSecurityEventNamesAreControlled(t *testing.T) {
	if AuthSecurityEvent("auth.login.ok").valid() {
		t.Fatal("arbitrary client names must be rejected")
	}
	if AuthSecurityEvent("custom.hack").valid() {
		t.Fatal("unlisted names must be rejected")
	}
	for _, e := range []AuthSecurityEvent{
		AuthEventLoginSuccess, AuthEventLoginFailed, AuthEventLogout,
		AuthEventSessionRevoked, AuthEventSessionsRevokedOthers,
		AuthEventPasswordResetCompleted, AuthEventPasskeyAdded, AuthEventPasskeyRemoved,
		AuthEventStepUpSuccess, AuthEventStepUpFailed, AuthEventRateLimitTriggered,
		AuthEventChallengeRequired, AuthEventChallengeFailed,
	} {
		if !e.valid() {
			t.Fatalf("required event %s must be valid", e)
		}
	}
}

func TestSecurityRecordRejectsSecretsAndUnknownTypes(t *testing.T) {
	rec := SecurityRecord{Type: "not-an-event", Result: AuthResultFailed}
	if err := rec.validate(); err == nil {
		t.Fatal("unknown type must fail")
	}
	rec = SecurityRecord{Type: AuthEventLoginFailed, Result: AuthResultFailed, AuthMethod: "otp"}
	if err := rec.validate(); err == nil {
		t.Fatal("otp auth method is not in scope")
	}
}

func TestUnknownAccountLoginEventOmitsIdentifier(t *testing.T) {
	sink := &MemorySecurityRecorder{}
	err := sink.Record(context.Background(), SecurityRecord{
		Type:       AuthEventLoginFailed,
		AuthMethod: AuthMethodPassword,
		Operation:  AuthOpPasswordLogin,
		Result:     AuthResultFailed,
		ReasonCode: ReasonUnknownOrInvalid,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := sink.Snapshot()
	if len(got) != 1 {
		t.Fatalf("n=%d", len(got))
	}
	if !got[0].UserID.IsZero() || got[0].AuthMethod != AuthMethodPassword {
		t.Fatalf("%+v", got[0])
	}
	raw, err := encodeAuthSecurityPayload(got[0], time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(raw))
	if strings.Contains(s, "nobody@") || strings.Contains(s, "secret-password") {
		t.Fatalf("payload leaked identifier or secret: %s", raw)
	}
	if strings.Contains(s, `"password"`) && !strings.Contains(s, `"auth_method":"password"`) {
		t.Fatalf("payload leaked password field: %s", raw)
	}
	if strings.Contains(s, "user_id") {
		t.Fatalf("unknown account must omit user_id: %s", raw)
	}
}

func TestAuthSecurityPayloadPassesOutboxSensitiveScan(t *testing.T) {
	user := mustSecurityID(t)
	session := mustSecurityID(t)
	raw, err := encodeAuthSecurityPayload(SecurityRecord{
		Type:         AuthEventLoginSuccess,
		UserID:       user,
		SessionID:    session,
		AuthMethod:   AuthMethodPasskey,
		Operation:    AuthOpPasskeyLoginFinish,
		Result:       AuthResultSuccess,
		RequestID:    "req-1",
		TraceID:      "cccccccccccccccccccccccccccccccc",
		RiskDecision: RiskAllow,
	}, time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	e := outbox.Event{
		ID:           mustOutboxID(t),
		EventType:    AuthSecurityEventType,
		EventVersion: AuthSecurityEventVersion,
		Payload:      raw,
		CreatedAt:    time.Now().UTC(),
		AvailableAt:  time.Now().UTC(),
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("outbox validate: %v payload=%s", err, raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"password", "otp", "token", "cookie", "secret", "email", "phone"} {
		if _, ok := m[banned]; ok {
			t.Fatalf("banned key %q", banned)
		}
	}
}

func TestOutboxSecurityRecorderDedupesRateLimit(t *testing.T) {
	enq := &memSecurityEnqueuer{keys: map[string]struct{}{}}
	rec, err := NewOutboxSecurityRecorder(enq, func() time.Time { return time.Unix(1_000_000, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	in := SecurityRecord{
		Type:       AuthEventRateLimitTriggered,
		Operation:  AuthOpPasswordLogin,
		Result:     AuthResultTriggered,
		ReasonCode: ReasonVelocityIP,
		DedupeKey:  "password_login|ip|hashed-subject",
	}
	if err := rec.Record(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if err := rec.Record(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if enq.n != 1 {
		t.Fatalf("writes = %d, want 1 (idempotent)", enq.n)
	}
}

func TestAuthSecurityHandlerLogsSafeFields(t *testing.T) {
	raw, err := encodeAuthSecurityPayload(SecurityRecord{
		Type:       AuthEventLogout,
		SessionID:  mustSecurityID(t),
		Result:     AuthResultSuccess,
		RequestID:  "req-safe",
		Operation:  AuthOpSessionRevoke,
		AuthMethod: AuthMethodPassword,
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	h := NewAuthSecurityHandler()
	err = h.Handle(context.Background(), outbox.Event{
		EventType:    AuthSecurityEventType,
		EventVersion: AuthSecurityEventVersion,
		Payload:      raw,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuthSecurityHandlerCompletesInvalidWithoutRetryStorm(t *testing.T) {
	h := NewAuthSecurityHandler()
	err := h.Handle(context.Background(), outbox.Event{
		EventType:    AuthSecurityEventType,
		EventVersion: AuthSecurityEventVersion,
		Payload:      json.RawMessage(`{"event":"not-real","result":"success"}`),
	})
	if err != nil {
		t.Fatalf("invalid payload must complete, not retry: %v", err)
	}
}

func mustSecurityID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustOutboxID(t *testing.T) outbox.ID {
	t.Helper()
	id, err := outbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type memSecurityEnqueuer struct {
	keys map[string]struct{}
	n    int
	fail error
}

func (e *memSecurityEnqueuer) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	if e.fail != nil {
		return outbox.Event{}, e.fail
	}
	if in.IdempotencyKey != "" {
		if _, ok := e.keys[in.IdempotencyKey]; ok {
			return outbox.Event{}, outbox.ErrConflict
		}
		e.keys[in.IdempotencyKey] = struct{}{}
	}
	if err := json.Unmarshal(in.Payload, &map[string]any{}); err != nil {
		return outbox.Event{}, err
	}
	e.n++
	return outbox.Event{EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
}

var _ SecurityRecorder = (*MemorySecurityRecorder)(nil)
var _ TxSecurityRecorder = (*MemorySecurityRecorder)(nil)
var _ SecurityRecorder = (*OutboxSecurityRecorder)(nil)
var _ TxSecurityRecorder = (*OutboxSecurityRecorder)(nil)

func TestOutboxRecorderMapsSensitivePayload(t *testing.T) {
	enq := &memSecurityEnqueuer{keys: map[string]struct{}{}, fail: outbox.ErrSensitivePayload}
	rec, err := NewOutboxSecurityRecorder(enq, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = rec.Record(context.Background(), SecurityRecord{Type: AuthEventLogout, Result: AuthResultSuccess})
	if err == nil || (!errors.Is(err, errInvalidAbusePolicy) && !errors.Is(err, errUnavailable)) {
		t.Fatalf("err = %v", err)
	}
}
