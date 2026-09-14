package notifications

import (
	"context"
	"errors"
	"strings"

	"backend/internal/notifications/contracts"
	"backend/internal/platform/outbox"
)

// IntentHandler consumes notifications.intent V1 outbox events.
// It upserts a delivery row, then invokes DeliveryService when a sender exists.
// Missing senders fail retryably; the handler never completes as a successful send.
type IntentHandler struct {
	store    deliveryStore
	delivery *DeliveryService
}

func NewIntentHandler(store deliveryStore, delivery *DeliveryService) (*IntentHandler, error) {
	if store == nil || delivery == nil {
		return nil, errStoreRequired
	}
	return &IntentHandler{store: store, delivery: delivery}, nil
}

func (h *IntentHandler) Handle(ctx context.Context, event outbox.Event) error {
	if h == nil || h.store == nil || h.delivery == nil {
		return errStoreRequired
	}
	if event.EventType != contracts.IntentEventType || event.EventVersion != contracts.IntentEventVersion {
		return errInvalidEvent
	}
	intent, err := contracts.DecodeIntent(event.Payload)
	if err != nil {
		return err
	}
	draft, err := deliveryFromIntent(intent, event)
	if err != nil {
		return err
	}
	stored, err := h.store.UpsertDelivery(ctx, draft)
	if err != nil {
		return mapStoreErr(err)
	}
	if stored.Status == StatusSent {
		return nil
	}
	if !h.delivery.HasSender(stored.Channel) {
		return errProviderRequired
	}
	if _, err := h.delivery.Deliver(ctx, stored); err != nil {
		_, _, giveUp := ClassifyProviderError(err)
		if giveUp {
			return nil
		}
		return mapStoreErr(err)
	}
	return nil
}

func deliveryFromIntent(intent contracts.Intent, event outbox.Event) (Delivery, error) {
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
		Channel:         intent.Channel,
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

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, errStoreRequired) ||
		errors.Is(err, errInvalidDelivery) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidEvent) || errors.Is(err, contracts.ErrInvalidIntent) ||
		errors.Is(err, contracts.ErrSensitivePayload) ||
		errors.Is(err, errMaterialUnusable) || errors.Is(err, errChannelMismatch) ||
		errors.Is(err, errProviderRequired) ||
		errors.Is(err, errProviderRetryable) || errors.Is(err, errProviderTimeout) ||
		errors.Is(err, errProviderUnconfigured) || errors.Is(err, errProviderPermanent) {
		return err
	}
	return errUnavailable
}

var _ outbox.Handler = (*IntentHandler)(nil)
