package notifications

import (
	"context"
	"strings"
	"time"

	"backend/internal/notifications/contracts"
	"backend/internal/platform/outbox"
)

// WarningHandler consumes notifications.moderation.warning V1.
// It records an in-app delivery and user-safe warning content. It does not
// send email or SMS.
type WarningHandler struct {
	store deliveryStore
}

func NewWarningHandler(store deliveryStore) (*WarningHandler, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	return &WarningHandler{store: store}, nil
}

func (h *WarningHandler) Handle(ctx context.Context, event outbox.Event) error {
	if h == nil || h.store == nil {
		return errStoreRequired
	}
	if event.EventType != contracts.WarningEventType || event.EventVersion != contracts.WarningEventVersion {
		return errInvalidEvent
	}
	intent, err := contracts.DecodeWarningIntent(event.Payload)
	if err != nil {
		return err
	}
	draft, err := deliveryFromWarning(intent, event)
	if err != nil {
		return err
	}
	stored, err := h.store.UpsertDelivery(ctx, draft)
	if err != nil {
		return mapStoreErr(err)
	}
	warning, err := warningFromIntent(intent)
	if err != nil {
		return err
	}
	if _, err := h.store.UpsertWarning(ctx, warning); err != nil {
		return mapStoreErr(err)
	}
	if stored.Status == StatusSent {
		return nil
	}
	now := intent.CreatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	stored.Status = StatusSent
	stored.UpdatedAt = now
	stored.CompletedAt = &now
	if _, err := h.store.SaveDelivery(ctx, stored); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func deliveryFromWarning(intent contracts.WarningIntent, event outbox.Event) (Delivery, error) {
	intentID, err := ParseID(intent.IntentID)
	if err != nil {
		return Delivery{}, errInvalidEvent
	}
	recipientID, err := ParseID(intent.Recipient.ID)
	if err != nil {
		return Delivery{}, errInvalidEvent
	}
	id, err := NewID()
	if err != nil {
		return Delivery{}, errUnavailable
	}
	d := Delivery{
		ID:              id,
		IntentID:        intentID,
		Channel:         contracts.ChannelInApp,
		TemplateCode:    intent.TemplateCode,
		TemplateVersion: 1,
		Locale:          intent.Locale,
		RecipientKind:   intent.Recipient.Kind,
		RecipientID:     recipientID,
		Status:          StatusPending,
		Attempts:        0,
		CreatedAt:       intent.CreatedAt.UTC(),
		UpdatedAt:       intent.CreatedAt.UTC(),
	}
	corr := strings.TrimSpace(intent.CorrelationID)
	if corr == "" && event.CorrelationID != nil {
		corr = strings.TrimSpace(*event.CorrelationID)
	}
	if corr != "" {
		d.CorrelationID = &corr
	}
	if err := d.Validate(); err != nil {
		return Delivery{}, err
	}
	return d, nil
}

func warningFromIntent(intent contracts.WarningIntent) (WarningRecord, error) {
	intentID, err := ParseID(intent.IntentID)
	if err != nil {
		return WarningRecord{}, errInvalidEvent
	}
	targetRef, err := ParseID(intent.TargetRef)
	if err != nil {
		return WarningRecord{}, errInvalidEvent
	}
	now := intent.CreatedAt.UTC()
	row := WarningRecord{
		IntentID:     intentID,
		Locale:       intent.Locale,
		TemplateCode: intent.TemplateCode,
		MessageKey:   intent.MessageKey,
		TargetType:   intent.TargetType,
		TargetRef:    targetRef,
		ReasonCode:   intent.ReasonCode,
		ActionAt:     intent.ActionAt.UTC(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := row.Validate(); err != nil {
		return WarningRecord{}, err
	}
	return row, nil
}

var _ outbox.Handler = (*WarningHandler)(nil)
