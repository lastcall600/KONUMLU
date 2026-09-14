package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/notifications/contracts"
	"backend/internal/platform/outbox"
)

func TestDeliveryValidate(t *testing.T) {
	d := validDelivery(t)
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	d.TemplateVersion = 0
	if err := d.Validate(); !errors.Is(err, errInvalidDelivery) {
		t.Fatalf("err = %v", err)
	}
}

func TestHandleCreatesDeliveryThenSendsEmail(t *testing.T) {
	store := NewMemoryStore()
	email := &stubEmail{ref: "email-1"}
	sms := &stubSMS{}
	h := mustSendingHandler(t, store, email, sms, emailMaterial())
	event := intentEvent(t, validIntentJSON(t))

	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if store.Len() != 1 {
		t.Fatalf("len = %d", store.Len())
	}
	intentID := mustIntentID(t)
	got, ok := store.GetByIntentID(intentID)
	if !ok {
		t.Fatal("missing delivery")
	}
	if got.Status != StatusSent || got.Channel != contracts.ChannelEmail {
		t.Fatalf("delivery = %+v", got)
	}
	if email.n != 1 || sms.n != 0 {
		t.Fatalf("email=%d sms=%d", email.n, sms.n)
	}
	if email.last.IdempotencyKey != got.ID.String() {
		t.Fatal("provider must receive stable delivery idempotency key")
	}
}

func TestHandleSMSRoute(t *testing.T) {
	store := NewMemoryStore()
	email := &stubEmail{}
	sms := &stubSMS{ref: "sms-1"}
	h := mustSendingHandler(t, store, email, sms, smsMaterial())
	if err := h.Handle(context.Background(), intentEvent(t, validSMSIntentJSON(t))); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetByIntentID(mustIntentID(t))
	if got.Status != StatusSent || sms.n != 1 || email.n != 0 {
		t.Fatalf("status=%s email=%d sms=%d", got.Status, email.n, sms.n)
	}
}

func TestHandleAlreadySentSkipsProvider(t *testing.T) {
	store := NewMemoryStore()
	email := &stubEmail{ref: "second"}
	h := mustSendingHandler(t, store, email, &stubSMS{}, emailMaterial())
	event := intentEvent(t, validIntentJSON(t))
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	first, _ := store.GetByIntentID(mustIntentID(t))
	email.n = 0

	event.ID = mustOutboxID(t)
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if email.n != 0 {
		t.Fatal("already-sent must not resend")
	}
	second, _ := store.GetByIntentID(mustIntentID(t))
	if second.ID != first.ID || second.Status != StatusSent {
		t.Fatalf("replay mutated delivery: %+v", second)
	}
}

func TestHandleUnavailableProviderIsRetryable(t *testing.T) {
	store := NewMemoryStore()
	h := mustHandler(t, store)
	err := h.Handle(context.Background(), intentEvent(t, validIntentJSON(t)))
	if !errors.Is(err, errProviderRequired) {
		t.Fatalf("err = %v", err)
	}
	got, ok := store.GetByIntentID(mustIntentID(t))
	if !ok {
		t.Fatal("delivery must still be recorded")
	}
	if got.Status == StatusSent {
		t.Fatal("unavailable provider must not mark sent")
	}
}

func TestHandleProviderFailureKeepsDeliveryRetryable(t *testing.T) {
	store := NewMemoryStore()
	email := &stubEmail{err: errors.New("provider down")}
	h := mustSendingHandler(t, store, email, &stubSMS{}, emailMaterial())
	err := h.Handle(context.Background(), intentEvent(t, validIntentJSON(t)))
	if !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
	got, _ := store.GetByIntentID(mustIntentID(t))
	if got.Status == StatusSent || got.CompletedAt != nil {
		t.Fatalf("must stay retryable: %+v", got)
	}
}

func TestHandlePermanentProviderCompletesWithoutRetry(t *testing.T) {
	store := NewMemoryStore()
	email := &stubEmail{err: errProviderPermanent}
	h := mustSendingHandler(t, store, email, &stubSMS{}, emailMaterial())
	if err := h.Handle(context.Background(), intentEvent(t, validIntentJSON(t))); err != nil {
		t.Fatalf("permanent must complete outbox: %v", err)
	}
	got, _ := store.GetByIntentID(mustIntentID(t))
	if got.Status != StatusFailed || got.CompletedAt != nil {
		t.Fatalf("permanent delivery = %+v", got)
	}
	if email.n != 1 {
		t.Fatalf("sends=%d", email.n)
	}
}

func TestHandleSuccessMarksSent(t *testing.T) {
	store := NewMemoryStore()
	h := mustSendingHandler(t, store, &stubEmail{ref: "ok"}, &stubSMS{}, emailMaterial())
	if err := h.Handle(context.Background(), intentEvent(t, validIntentJSON(t))); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetByIntentID(mustIntentID(t))
	if got.Status != StatusSent || got.CompletedAt == nil {
		t.Fatalf("got = %+v", got)
	}
}

func TestHandleReplayedEventIsSafe(t *testing.T) {
	store := NewMemoryStore()
	email := &stubEmail{ref: "once"}
	h := mustSendingHandler(t, store, email, &stubSMS{}, emailMaterial())
	event := intentEvent(t, validIntentJSON(t))
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if store.Len() != 1 || email.n != 1 {
		t.Fatalf("len=%d sends=%d", store.Len(), email.n)
	}
}

func TestHandleDoesNotLeakSecret(t *testing.T) {
	secret := "super-secret-otp-999111"
	dest := "leak@example.com"
	store := NewMemoryStore()
	email := &stubEmail{err: fmt.Errorf("send to %s secret=%s", dest, secret)}
	h := mustSendingHandler(t, store, email, &stubSMS{}, VerificationMaterial{
		Kind:        MaterialKindEmail,
		Destination: dest,
		Secret:      secret,
	})
	err := h.Handle(context.Background(), intentEvent(t, validIntentJSON(t)))
	if err == nil {
		t.Fatal("expected error")
	}
	assertNoLeak(t, err, secret, dest)
	got, _ := store.GetByIntentID(mustIntentID(t))
	if got.ProviderRef != nil && strings.Contains(*got.ProviderRef, secret) {
		t.Fatal("delivery must not store secret")
	}
}

func TestHandleRejectsWrongEventVersion(t *testing.T) {
	h := mustHandler(t, NewMemoryStore())
	event := intentEvent(t, validIntentJSON(t))
	event.EventVersion = 2
	if err := h.Handle(context.Background(), event); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("err = %v", err)
	}
}

func TestHandleRejectsMalformedPayload(t *testing.T) {
	h := mustHandler(t, NewMemoryStore())
	event := intentEvent(t, json.RawMessage(`{"not":"an intent"}`))
	if err := h.Handle(context.Background(), event); !errors.Is(err, contracts.ErrInvalidIntent) {
		t.Fatalf("err = %v", err)
	}
}

func TestHandleRejectsSecretFields(t *testing.T) {
	store := NewMemoryStore()
	h := mustHandler(t, store)
	raw := json.RawMessage(`{
		"intent_id":"11111111-1111-4111-8111-111111111111",
		"version":1,
		"purpose":"security",
		"template_code":"identity.verification.signup",
		"channel":"sms",
		"locale":"tr",
		"recipient":{"kind":"verification_challenge","id":"22222222-2222-4222-8222-222222222222"},
		"created_at":"2026-09-06T12:00:00Z",
		"otp":"123456"
	}`)
	if err := h.Handle(context.Background(), intentEvent(t, raw)); !errors.Is(err, contracts.ErrSensitivePayload) {
		t.Fatalf("err = %v", err)
	}
	if store.Len() != 0 {
		t.Fatal("sensitive payload must not create a delivery")
	}
}

func TestHandleStoreFailure(t *testing.T) {
	store := NewMemoryStore()
	store.SetFail(errors.New("disk full"))
	h := mustHandler(t, store)
	if err := h.Handle(context.Background(), intentEvent(t, validIntentJSON(t))); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestHandlerAndStoreFailureReschedulesThroughRelay(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	obStore := newRelayMemStore()
	svc, err := outbox.New(obStore, outbox.Policy{
		BatchSize:         8,
		Lease:             15 * time.Second,
		BackoffBase:       time.Minute,
		BackoffMultiplier: 2,
		BackoffCap:        10 * time.Minute,
		Jitter:            0,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	payload := validIntentJSON(t)
	event, err := svc.Enqueue(ctx, nil, outbox.NewEvent{
		EventType:    contracts.IntentEventType,
		EventVersion: contracts.IntentEventVersion,
		Payload:      payload,
	})
	if err != nil {
		t.Fatal(err)
	}

	failing := NewMemoryStore()
	failing.SetFail(errors.New("write failed"))
	delivery, err := NewDeliveryService(failing, &stubResolver{material: emailMaterial()}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewIntentHandler(failing, delivery)
	if err != nil {
		t.Fatal(err)
	}
	reg := outbox.NewRegistry()
	if err := reg.Register(contracts.IntentEventType, contracts.IntentEventVersion, h); err != nil {
		t.Fatal(err)
	}
	relay, err := outbox.NewRelay(svc, reg, time.Second, now)
	if err != nil {
		t.Fatal(err)
	}
	ctxRun, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctxRun) }()

	deadline := time.Now().Add(2 * time.Second)
	var row outbox.Event
	for time.Now().Before(deadline) {
		row = obStore.get(t, event.ID)
		if row.LastErrorClass != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay did not stop")
	}
	if row.CompletedAt != nil {
		t.Fatal("store failure must not complete the outbox event")
	}
	if row.LastErrorClass == nil || *row.LastErrorClass != outbox.ErrorClassHandlerFailed {
		t.Fatalf("error class = %v", row.LastErrorClass)
	}
}

func TestHandleProviderFailureReschedulesThroughRelay(t *testing.T) {
	ctx := context.Background()
	now := func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	obStore := newRelayMemStore()
	svc, err := outbox.New(obStore, outbox.Policy{
		BatchSize:         8,
		Lease:             15 * time.Second,
		BackoffBase:       time.Minute,
		BackoffMultiplier: 2,
		BackoffCap:        10 * time.Minute,
		Jitter:            0,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	event, err := svc.Enqueue(ctx, nil, outbox.NewEvent{
		EventType:    contracts.IntentEventType,
		EventVersion: contracts.IntentEventVersion,
		Payload:      validIntentJSON(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	store := NewMemoryStore()
	h := mustSendingHandler(t, store, &stubEmail{err: errors.New("smtp timeout")}, &stubSMS{}, emailMaterial())
	reg := outbox.NewRegistry()
	if err := reg.Register(contracts.IntentEventType, contracts.IntentEventVersion, h); err != nil {
		t.Fatal(err)
	}
	relay, err := outbox.NewRelay(svc, reg, time.Second, now)
	if err != nil {
		t.Fatal(err)
	}
	ctxRun, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctxRun) }()

	deadline := time.Now().Add(2 * time.Second)
	var row outbox.Event
	for time.Now().Before(deadline) {
		row = obStore.get(t, event.ID)
		if row.LastErrorClass != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay did not stop")
	}
	if row.CompletedAt != nil {
		t.Fatal("provider failure must not complete the outbox event")
	}
	if row.LastErrorClass == nil || *row.LastErrorClass != outbox.ErrorClassHandlerFailed {
		t.Fatalf("error class = %v", row.LastErrorClass)
	}
	got, _ := store.GetByIntentID(mustIntentID(t))
	if got.Status == StatusSent {
		t.Fatal("delivery must not be sent")
	}
}

func mustHandler(t *testing.T, store deliveryStore) *IntentHandler {
	t.Helper()
	return mustSendingHandler(t, store, nil, nil, emailMaterial())
}

func mustSendingHandler(t *testing.T, store deliveryStore, email EmailSender, sms SMSSender, material VerificationMaterial) *IntentHandler {
	t.Helper()
	now := time.Date(2026, 9, 6, 12, 1, 0, 0, time.UTC)
	svc, err := NewDeliveryService(store, &stubResolver{material: material}, email, sms, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewIntentHandler(store, svc)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustIntentID(t *testing.T) ID {
	t.Helper()
	id, err := ParseID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func emailMaterial() VerificationMaterial {
	return VerificationMaterial{Kind: MaterialKindEmail, Destination: "owner@example.com", Secret: "email-token-secret"}
}

func smsMaterial() VerificationMaterial {
	return VerificationMaterial{Kind: MaterialKindPhone, Destination: "+15551234567", Secret: "123456"}
}

func validIntentJSON(t *testing.T) json.RawMessage {
	t.Helper()
	return marshalIntent(t, contracts.ChannelEmail)
}

func validSMSIntentJSON(t *testing.T) json.RawMessage {
	t.Helper()
	return marshalIntent(t, contracts.ChannelSMS)
}

func marshalIntent(t *testing.T, channel contracts.Channel) json.RawMessage {
	t.Helper()
	i := contracts.Intent{
		IntentID:     "11111111-1111-4111-8111-111111111111",
		Version:      contracts.IntentEventVersion,
		Purpose:      contracts.PurposeSecurity,
		TemplateCode: contracts.TemplateIdentityVerificationSignup,
		Channel:      channel,
		Locale:       contracts.LocaleTR,
		Recipient: contracts.RecipientRef{
			Kind: contracts.RecipientVerificationChallenge,
			ID:   "22222222-2222-4222-8222-222222222222",
		},
		CorrelationID: "corr-1",
		CreatedAt:     time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(i)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func intentEvent(t *testing.T, payload json.RawMessage) outbox.Event {
	t.Helper()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	return outbox.Event{
		ID:           mustOutboxID(t),
		EventType:    contracts.IntentEventType,
		EventVersion: contracts.IntentEventVersion,
		Payload:      payload,
		CreatedAt:    now,
		AvailableAt:  now,
	}
}

func mustOutboxID(t *testing.T) outbox.ID {
	t.Helper()
	id, err := outbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func validDelivery(t *testing.T) Delivery {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	intentID, err := ParseID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	recipientID, err := ParseID("22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	return Delivery{
		ID:              id,
		IntentID:        intentID,
		Channel:         contracts.ChannelSMS,
		TemplateCode:    contracts.TemplateIdentityVerificationSignup,
		TemplateVersion: 1,
		Locale:          contracts.LocaleTR,
		RecipientKind:   contracts.RecipientVerificationChallenge,
		RecipientID:     recipientID,
		Status:          StatusPending,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

type relayMemStore struct {
	mu   sync.Mutex
	rows map[outbox.ID]outbox.Event
}

func newRelayMemStore() *relayMemStore {
	return &relayMemStore{rows: make(map[outbox.ID]outbox.Event)}
}

func (m *relayMemStore) Insert(_ context.Context, _ outbox.Execer, event outbox.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[event.ID] = event
	return nil
}

func (m *relayMemStore) Claim(_ context.Context, now time.Time, lease time.Duration, limit int) ([]outbox.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []outbox.Event
	for id, e := range m.rows {
		if e.CompletedAt != nil {
			continue
		}
		if now.Before(e.AvailableAt) {
			continue
		}
		if e.ClaimUntil != nil && now.Before(*e.ClaimUntil) {
			continue
		}
		until := now.Add(lease)
		e.ClaimedAt = &now
		e.ClaimUntil = &until
		e.Attempts++
		m.rows[id] = e
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *relayMemStore) Complete(_ context.Context, id outbox.ID, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok {
		return outbox.ErrNotFound
	}
	e.CompletedAt = &now
	m.rows[id] = e
	return nil
}

func (m *relayMemStore) Reschedule(_ context.Context, id outbox.ID, availableAt time.Time, errorClass string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok {
		return outbox.ErrNotFound
	}
	e.AvailableAt = availableAt
	e.ClaimedAt = nil
	e.ClaimUntil = nil
	e.LastErrorClass = &errorClass
	m.rows[id] = e
	return nil
}

func (m *relayMemStore) IncrementAttempts(_ context.Context, id outbox.ID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok {
		return 0, outbox.ErrNotFound
	}
	e.Attempts++
	m.rows[id] = e
	return e.Attempts, nil
}

func (m *relayMemStore) get(t *testing.T, id outbox.ID) outbox.Event {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok {
		t.Fatalf("missing %s", id)
	}
	return e
}
