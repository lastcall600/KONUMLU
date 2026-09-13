package main

import (
	"context"
	"strings"

	delcontracts "backend/internal/deliveries/contracts"
	dispcontracts "backend/internal/disputes/contracts"
	msgcontracts "backend/internal/messaging/contracts"
	"backend/internal/notifications"
	"backend/internal/notifications/policy"
	offercontracts "backend/internal/offers/contracts"
	"backend/internal/platform/observability"
	"backend/internal/platform/outbox"
	txncontracts "backend/internal/transactions/contracts"
)

func newMarketplaceNotifyHandler(m *notifications.Materializer) outbox.Handler {
	return outbox.HandlerFunc(func(ctx context.Context, event outbox.Event) error {
		switch event.EventType {
		case msgcontracts.EventTypeMessageReceived:
			p, err := msgcontracts.DecodeReceived(event.Payload)
			if err != nil {
				observability.FromContext(ctx).Info("notification_producer_invalid", "event_type", event.EventType, "error_class", "invalid_event")
				return nil
			}
			return notifications.MaterializeFanout(ctx, m, policy.EventMessagingMessageReceived, p.SenderUserID, p.MessageID, p.ConversationID, []string{p.RecipientUserID}, map[string]string{
				"conversation_id": p.ConversationID,
				"message_id":      p.MessageID,
			})
		case offercontracts.EventTypeSubmitted:
			return handleOffer(ctx, m, event, policy.EventOfferReceived, true)
		case offercontracts.EventTypeAccepted:
			return handleOffer(ctx, m, event, policy.EventOfferAccepted, false)
		case offercontracts.EventTypeRejected:
			return handleOffer(ctx, m, event, policy.EventOfferRejected, false)
		case txncontracts.EventTypeCreated:
			p, err := txncontracts.DecodeLifecycle(event.Payload)
			if err != nil {
				observability.FromContext(ctx).Info("notification_producer_invalid", "event_type", event.EventType, "error_class", "invalid_event")
				return nil
			}
			return notifications.MaterializeFanout(ctx, m, policy.EventTransactionCreated, p.ActorUserID, p.TransactionID+":created", p.TransactionID, []string{p.RequesterUserID, p.ProviderUserID}, map[string]string{
				"transaction_id": p.TransactionID,
			})
		case txncontracts.EventTypeCompleted:
			p, err := txncontracts.DecodeCompleted(event.Payload)
			if err != nil {
				observability.FromContext(ctx).Info("notification_producer_invalid", "event_type", event.EventType, "error_class", "invalid_event")
				return nil
			}
			recipients := []string{p.RequesterUserID}
			if strings.TrimSpace(p.ProviderUserID) != "" {
				recipients = append(recipients, p.ProviderUserID)
			}
			return notifications.MaterializeFanout(ctx, m, policy.EventTransactionCompleted, p.ActorUserID, p.TransactionID+":completed", p.TransactionID, recipients, map[string]string{
				"transaction_id": p.TransactionID,
			})
		case txncontracts.EventTypeCancelled:
			p, err := txncontracts.DecodeLifecycle(event.Payload)
			if err != nil {
				observability.FromContext(ctx).Info("notification_producer_invalid", "event_type", event.EventType, "error_class", "invalid_event")
				return nil
			}
			return notifications.MaterializeFanout(ctx, m, policy.EventTransactionCancelled, p.ActorUserID, p.TransactionID+":cancelled", p.TransactionID, []string{p.RequesterUserID, p.ProviderUserID}, map[string]string{
				"transaction_id": p.TransactionID,
			})
		case delcontracts.EventTypeStatusChanged:
			p, err := delcontracts.DecodeStatusChanged(event.Payload)
			if err != nil {
				observability.FromContext(ctx).Info("notification_producer_invalid", "event_type", event.EventType, "error_class", "invalid_event")
				return nil
			}
			return notifications.MaterializeFanout(ctx, m, policy.EventDeliveryStatusChanged, p.ActorUserID, p.DeliveryID+":"+p.StatusCode, p.DeliveryID, []string{p.RequesterUserID, p.ProviderUserID}, map[string]string{
				"delivery_id": p.DeliveryID,
				"status_code": p.StatusCode,
			})
		case dispcontracts.EventTypeUpdated:
			p, err := dispcontracts.DecodeUpdated(event.Payload)
			if err != nil {
				observability.FromContext(ctx).Info("notification_producer_invalid", "event_type", event.EventType, "error_class", "invalid_event")
				return nil
			}
			return notifications.MaterializeFanout(ctx, m, policy.EventDisputeUpdated, p.ActorUserID, p.DisputeID+":"+p.StatusCode, p.DisputeID, []string{p.RequesterUserID, p.ProviderUserID}, map[string]string{
				"dispute_id":  p.DisputeID,
				"status_code": p.StatusCode,
			})
		default:
			return notifications.ErrInvalidEvent
		}
	})
}

func handleOffer(ctx context.Context, m *notifications.Materializer, event outbox.Event, catalogEvent policy.EventType, notifyRequester bool) error {
	p, err := offercontracts.DecodeTransition(event.Payload)
	if err != nil {
		observability.FromContext(ctx).Info("notification_producer_invalid", "event_type", event.EventType, "error_class", "invalid_event")
		return nil
	}
	recipients := []string{p.ProviderUserID}
	if notifyRequester {
		recipients = []string{p.RequesterUserID}
	}
	return notifications.MaterializeFanout(ctx, m, catalogEvent, p.ActorUserID, p.OfferID+":"+p.Transition, p.OfferID, recipients, map[string]string{
		"offer_id": p.OfferID,
		"need_id":  p.NeedID,
	})
}

func registerMarketplaceNotify(reg *outbox.Registry, h outbox.Handler) error {
	if h == nil {
		return notifications.ErrStoreRequired
	}
	events := []struct {
		typ     string
		version int
	}{
		{msgcontracts.EventTypeMessageReceived, msgcontracts.EventVersion},
		{offercontracts.EventTypeSubmitted, offercontracts.EventVersion},
		{offercontracts.EventTypeAccepted, offercontracts.EventVersion},
		{offercontracts.EventTypeRejected, offercontracts.EventVersion},
		{txncontracts.EventTypeCreated, txncontracts.EventVersion},
		{txncontracts.EventTypeCancelled, txncontracts.EventVersion},
		{delcontracts.EventTypeStatusChanged, delcontracts.EventVersion},
		{dispcontracts.EventTypeUpdated, dispcontracts.EventVersion},
	}
	for _, e := range events {
		if err := reg.Register(e.typ, e.version, h); err != nil {
			return err
		}
	}
	return nil
}

func marketplaceCompleted(h outbox.Handler) outbox.Handler {
	if h != nil {
		return h
	}
	return outbox.HandlerFunc(func(context.Context, outbox.Event) error { return nil })
}
