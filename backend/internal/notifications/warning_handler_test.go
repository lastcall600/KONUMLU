package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/notifications/contracts"
	"backend/internal/platform/outbox"
)

func TestWarningHandlerRecordsInAppWithoutEmailSMS(t *testing.T) {
	store := NewMemoryStore()
	h, err := NewWarningHandler(store)
	if err != nil {
		t.Fatal(err)
	}
	event := warningEvent(t, mustWarningJSON(t, validWarningIntent()))
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	intentID := mustParseNotifyID(t, validWarningIntent().IntentID)
	got, ok := store.GetByIntentID(intentID)
	if !ok || got.Status != StatusSent || got.Channel != contracts.ChannelInApp {
		t.Fatalf("delivery = %+v ok=%v", got, ok)
	}
	if got.RecipientKind != contracts.RecipientUser || got.RecipientID.String() != validWarningIntent().Recipient.ID {
		t.Fatalf("recipient = %+v", got)
	}
	warn, ok := store.GetWarning(intentID)
	if !ok {
		t.Fatal("missing warning record")
	}
	if warn.MessageKey != contracts.MessageKeyWarningListing || warn.ReasonCode != contracts.WarningReasonPolicyViolation {
		t.Fatalf("warning = %+v", warn)
	}
	if store.WarningLen() != 1 {
		t.Fatalf("warnings = %d", store.WarningLen())
	}
}

func TestWarningHandlerReplayIsIdempotent(t *testing.T) {
	store := NewMemoryStore()
	h, err := NewWarningHandler(store)
	if err != nil {
		t.Fatal(err)
	}
	event := warningEvent(t, mustWarningJSON(t, validWarningIntent()))
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	first, _ := store.GetByIntentID(mustParseNotifyID(t, validWarningIntent().IntentID))
	event.ID = mustOutboxID(t)
	if err := h.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	second, _ := store.GetByIntentID(mustParseNotifyID(t, validWarningIntent().IntentID))
	if second.ID != first.ID || second.Status != StatusSent {
		t.Fatalf("replay mutated delivery: %+v", second)
	}
	if store.WarningLen() != 1 || store.Len() != 1 {
		t.Fatalf("dup rows delivery=%d warning=%d", store.Len(), store.WarningLen())
	}
}

func TestWarningHandlerRejectsLeaksAndWrongEvent(t *testing.T) {
	store := NewMemoryStore()
	h, err := NewWarningHandler(store)
	if err != nil {
		t.Fatal(err)
	}
	ev := warningEvent(t, json.RawMessage(`{"reporter":"x"}`))
	if err := h.Handle(context.Background(), ev); !errors.Is(err, contracts.ErrSensitivePayload) && !errors.Is(err, contracts.ErrInvalidIntent) {
		t.Fatalf("leak err = %v", err)
	}
	ev = warningEvent(t, mustWarningJSON(t, validWarningIntent()))
	ev.EventType = contracts.IntentEventType
	if err := h.Handle(context.Background(), ev); !errors.Is(err, errInvalidEvent) {
		t.Fatalf("wrong type err = %v", err)
	}
}

func TestWarningUserPayloadOmitsInternalIDs(t *testing.T) {
	intent := validWarningIntent()
	body, err := json.Marshal(intent.UserPayload())
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(body))
	for _, leak := range []string{"recipient", "reporter", "staff", "rationale", "evidence", intent.Recipient.ID, "intent_id"} {
		if strings.Contains(s, strings.ToLower(leak)) {
			t.Fatalf("payload leaked %q: %s", leak, body)
		}
	}
}

func validWarningIntent() contracts.WarningIntent {
	return contracts.WarningIntent{
		IntentID:     "11111111-1111-4111-8111-111111111111",
		Version:      contracts.WarningEventVersion,
		Purpose:      contracts.PurposeTransactional,
		TemplateCode: contracts.TemplateModerationWarningIssued,
		Channel:      contracts.ChannelInApp,
		Locale:       contracts.LocaleTR,
		Recipient: contracts.RecipientRef{
			Kind: contracts.RecipientUser,
			ID:   "22222222-2222-4222-8222-222222222222",
		},
		MessageKey: contracts.MessageKeyWarningListing,
		TargetType: contracts.WarningTargetListing,
		TargetRef:  "33333333-3333-4333-8333-333333333333",
		ReasonCode: contracts.WarningReasonPolicyViolation,
		ActionAt:   time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
	}
}

func mustWarningJSON(t *testing.T, w contracts.WarningIntent) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func warningEvent(t *testing.T, payload json.RawMessage) outbox.Event {
	t.Helper()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	return outbox.Event{
		ID:           mustOutboxID(t),
		EventType:    contracts.WarningEventType,
		EventVersion: contracts.WarningEventVersion,
		Payload:      payload,
		CreatedAt:    now,
		AvailableAt:  now,
	}
}

func mustParseNotifyID(t *testing.T, s string) ID {
	t.Helper()
	id, err := ParseID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
