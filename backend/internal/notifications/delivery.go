package notifications

import (
	"context"
	"errors"
	"time"

	"backend/internal/notifications/contracts"
	"backend/internal/platform/observability"
)

// DeliveryService orchestrates verification delivery.
//
// Semantics are at-least-once, never exactly-once. The stable idempotency key
// passed to providers is the delivery ID. Already-sent rows are not resent.
// If the provider accepts and the process crashes before status=sent is stored,
// the next attempt may call the provider again. Provider adapters MUST use the
// idempotency key where the vendor supports it. Verification material is not
// destroyed after send.
type DeliveryService struct {
	store    deliveryStore
	resolver VerificationMaterialResolver
	email    EmailSender
	sms      SMSSender
	now      func() time.Time
}

func NewDeliveryService(store deliveryStore, resolver VerificationMaterialResolver, email EmailSender, sms SMSSender, now func() time.Time) (*DeliveryService, error) {
	if store == nil || resolver == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &DeliveryService{store: store, resolver: resolver, email: email, sms: sms, now: now}, nil
}

// HasSender reports whether a real adapter is wired for the channel.
// A missing sender is not a successful no-op; callers must fail retryably.
func (s *DeliveryService) HasSender(channel contracts.Channel) bool {
	if s == nil {
		return false
	}
	switch channel {
	case contracts.ChannelEmail:
		return s.email != nil
	case contracts.ChannelSMS:
		return s.sms != nil
	default:
		return false
	}
}

// Deliver sends one verification_challenge delivery. Material is not destroyed after send.
func (s *DeliveryService) Deliver(ctx context.Context, delivery Delivery) (Delivery, error) {
	if s == nil || s.store == nil || s.resolver == nil {
		return Delivery{}, errStoreRequired
	}
	if err := delivery.Validate(); err != nil {
		return Delivery{}, err
	}
	if delivery.Status == StatusSent {
		return delivery, nil
	}
	if delivery.RecipientKind != contracts.RecipientVerificationChallenge {
		return Delivery{}, errInvalidDelivery
	}

	mat, err := s.resolver.Resolve(ctx, delivery.RecipientID)
	if err != nil {
		return Delivery{}, sanitizeResolveErr(err)
	}
	if !mat.valid() {
		return Delivery{}, errMaterialUnusable
	}
	if !mat.Kind.Matches(delivery.Channel) {
		return Delivery{}, errChannelMismatch
	}

	now := s.now().UTC()
	delivery.Attempts++
	delivery.LastAttemptAt = &now
	delivery.UpdatedAt = now

	ref, err := s.send(ctx, delivery, mat)
	if err != nil {
		delivery.Status = StatusFailed
		saved, saveErr := s.store.SaveDelivery(ctx, delivery)
		if saveErr != nil {
			return Delivery{}, mapStoreErr(saveErr)
		}
		return saved, sanitizeProviderErr(err)
	}

	delivery.Status = StatusSent
	if ref != "" {
		delivery.ProviderRef = &ref
	}
	delivery.CompletedAt = &now
	saved, err := s.store.SaveDelivery(ctx, delivery)
	if err != nil {
		return Delivery{}, mapStoreErr(err)
	}
	return saved, nil
}

func (s *DeliveryService) send(ctx context.Context, delivery Delivery, mat VerificationMaterial) (string, error) {
	start := time.Now()
	name, ref, err := s.invokeProvider(ctx, delivery, mat)
	if name != "" {
		observability.LogProvider(ctx, name, time.Since(start), err)
	}
	return ref, err
}

func (s *DeliveryService) invokeProvider(ctx context.Context, delivery Delivery, mat VerificationMaterial) (string, string, error) {
	key := delivery.ID.String()
	switch delivery.Channel {
	case contracts.ChannelEmail:
		if s.email == nil {
			return "email", "", errProviderRequired
		}
		req := EmailSendRequest{
			Destination:        mat.Destination,
			TemplateCode:       delivery.TemplateCode,
			TemplateVersion:    delivery.TemplateVersion,
			Locale:             delivery.Locale,
			VerificationSecret: mat.Secret,
			IdempotencyKey:     key,
		}
		if !req.valid() {
			return "email", "", errInvalidDelivery
		}
		res, err := s.email.Send(ctx, req)
		if err != nil {
			return "email", "", err
		}
		return "email", res.ProviderRef, nil
	case contracts.ChannelSMS:
		if s.sms == nil {
			return "sms", "", errProviderRequired
		}
		req := SMSSendRequest{
			Destination:        mat.Destination,
			TemplateCode:       delivery.TemplateCode,
			TemplateVersion:    delivery.TemplateVersion,
			Locale:             delivery.Locale,
			VerificationSecret: mat.Secret,
			IdempotencyKey:     key,
		}
		if !req.valid() {
			return "sms", "", errInvalidDelivery
		}
		res, err := s.sms.Send(ctx, req)
		if err != nil {
			return "sms", "", err
		}
		return "sms", res.ProviderRef, nil
	default:
		return "", "", errInvalidDelivery
	}
}

func sanitizeResolveErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errMaterialUnusable) || errors.Is(err, errUnavailable) ||
		errors.Is(err, errStoreRequired) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidDelivery) {
		return err
	}
	return errUnavailable
}

func sanitizeProviderErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errProviderRequired) || errors.Is(err, errUnavailable) ||
		errors.Is(err, errInvalidDelivery) {
		return err
	}
	return errUnavailable
}
