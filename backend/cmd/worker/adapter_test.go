package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"backend/internal/identity"
	notifyinfra "backend/internal/infrastructure/notifications"
	"backend/internal/notifications"
	"backend/internal/notifications/contracts"
	"backend/internal/platform/config"
)

func TestIdentityMaterialResolverMapsEmailAndPhone(t *testing.T) {
	src := &stubIdentitySource{
		byID: map[identity.ID]identity.VerificationDelivery{},
	}
	emailID := mustNotifyAsIdentity(t)
	phoneID := mustNotifyAsIdentity(t)
	src.byID[emailID] = identity.VerificationDelivery{
		Kind:                 identity.IdentifierEmail,
		DestinationCanonical: "owner@example.com",
		Secret:               "email-token",
	}
	src.byID[phoneID] = identity.VerificationDelivery{
		Kind:                 identity.IdentifierPhone,
		DestinationCanonical: "+15551234567",
		Secret:               "654321",
	}
	r, err := newIdentityMaterialResolver(src)
	if err != nil {
		t.Fatal(err)
	}

	email, err := r.Resolve(context.Background(), notifications.ID(emailID))
	if err != nil {
		t.Fatal(err)
	}
	if email.Kind != notifications.MaterialKindEmail || email.Destination != "owner@example.com" || email.Secret != "email-token" {
		t.Fatalf("email = %+v", email)
	}

	phone, err := r.Resolve(context.Background(), notifications.ID(phoneID))
	if err != nil {
		t.Fatal(err)
	}
	if phone.Kind != notifications.MaterialKindPhone || phone.Destination != "+15551234567" || phone.Secret != "654321" {
		t.Fatalf("phone = %+v", phone)
	}
}

func TestIdentityMaterialResolverMapsUnusableAndDoesNotLeak(t *testing.T) {
	secret := "raw-secret-value"
	dest := "hide@example.com"
	src := &stubIdentitySource{err: fmt.Errorf("%w dest=%s secret=%s", identity.ErrChallengeExpired, dest, secret)}
	r, err := newIdentityMaterialResolver(src)
	if err != nil {
		t.Fatal(err)
	}
	id := mustNotifyAsIdentity(t)
	_, got := r.Resolve(context.Background(), notifications.ID(id))
	if !errors.Is(got, notifications.ErrMaterialUnusable) {
		t.Fatalf("err = %v", got)
	}
	if strings.Contains(got.Error(), secret) || strings.Contains(got.Error(), dest) {
		t.Fatalf("adapter leaked PII: %v", got)
	}

	src.err = identity.ErrChallengeConsumed
	if _, err := r.Resolve(context.Background(), notifications.ID(id)); !errors.Is(err, notifications.ErrMaterialUnusable) {
		t.Fatalf("consumed err = %v", err)
	}
	src.err = identity.ErrUnavailable
	if _, err := r.Resolve(context.Background(), notifications.ID(id)); !errors.Is(err, notifications.ErrUnavailable) {
		t.Fatalf("unavailable err = %v", err)
	}
}

func TestNewHandlerRegistryDoesNotRegisterFakeProvider(t *testing.T) {
	resolver, err := newIdentityMaterialResolver(&stubIdentitySource{})
	if err != nil {
		t.Fatal(err)
	}
	wiring := mustDisabledWiring(t, resolver)
	if wiring.Email != nil || wiring.SMS != nil {
		t.Fatal("cmd/worker production wiring must not include a fake email/SMS provider")
	}
	if _, ok := wiring.Resolver.(*identityMaterialResolver); !ok {
		t.Fatal("worker resolver must be the Identity adapter")
	}
	store := notifications.NewMemoryStore()
	delivery, err := notifications.NewDeliveryService(store, wiring.Resolver, wiring.Email, wiring.SMS, nil)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.HasSender(contracts.ChannelEmail) || delivery.HasSender(contracts.ChannelSMS) {
		t.Fatal("production delivery service must not expose a sender")
	}
	reg, err := newHandlerRegistry(store, delivery, mustTestMediaHandler(t), mustTestSearchHandler(t), mustTestTrustHandler(t), mustTestReviewAggregatesHandler(t), mustTestCompletionHandler(t), noopNotifySecurity(), nil)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := reg.Lookup("notifications.intent", 1)
	if !ok || h == nil {
		t.Fatal("intent handler required")
	}
}

func TestExternalModeWithoutAdapterFailsWiring(t *testing.T) {
	resolver, err := newIdentityMaterialResolver(&stubIdentitySource{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = productionNotificationsWiring(config.Config{
		NotificationsEmailMode: config.NotificationChannelExternal,
		NotificationsSMSMode:   config.NotificationChannelDisabled,
	}, resolver)
	if !errors.Is(err, notifyinfra.ErrEmailAdapterRequired) {
		t.Fatalf("email external err = %v", err)
	}
	_, err = productionNotificationsWiring(config.Config{
		NotificationsEmailMode: config.NotificationChannelDisabled,
		NotificationsSMSMode:   config.NotificationChannelExternal,
	}, resolver)
	if !errors.Is(err, notifyinfra.ErrSMSAdapterRequired) {
		t.Fatalf("sms external err = %v", err)
	}
}

func TestAdapterReceivesIdempotencyKey(t *testing.T) {
	rec := &recordingEmailClient{ref: "email-ref"}
	a, err := notifyinfra.NewEmailAdapter(rec)
	if err != nil {
		t.Fatal(err)
	}
	key := "stable-delivery-id"
	_, err = a.Send(context.Background(), notifications.EmailSendRequest{
		Destination:        "owner@example.com",
		TemplateCode:       "identity.verification.signup",
		TemplateVersion:    1,
		Locale:             "tr",
		VerificationSecret: "email-token",
		IdempotencyKey:     key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.last.IdempotencyKey != key {
		t.Fatalf("idempotency = %q", rec.last.IdempotencyKey)
	}
}

func TestProviderErrorMapsRetryablyWithoutLeak(t *testing.T) {
	dest := "owner@example.com"
	secret := "otp-or-token"
	rec := &recordingEmailClient{err: fmt.Errorf("vendor 503 dest=%s token=%s body={\"raw\":true}", dest, secret)}
	a, err := notifyinfra.NewEmailAdapter(rec)
	if err != nil {
		t.Fatal(err)
	}
	_, got := a.Send(context.Background(), notifications.EmailSendRequest{IdempotencyKey: "k"})
	if !errors.Is(got, notifications.ErrUnavailable) {
		t.Fatalf("err = %v", got)
	}
	msg := got.Error()
	if strings.Contains(msg, dest) || strings.Contains(msg, secret) || strings.Contains(msg, "vendor 503") {
		t.Fatalf("leaked provider detail: %v", got)
	}
}

func mustDisabledWiring(t *testing.T, resolver notifications.VerificationMaterialResolver) notificationsWiring {
	t.Helper()
	wiring, err := productionNotificationsWiring(config.Config{
		NotificationsEmailMode: config.NotificationChannelDisabled,
		NotificationsSMSMode:   config.NotificationChannelDisabled,
	}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	return wiring
}

type recordingEmailClient struct {
	last notifications.EmailSendRequest
	ref  string
	err  error
}

func (r *recordingEmailClient) Send(_ context.Context, req notifications.EmailSendRequest) (notifications.SendResult, error) {
	r.last = req
	if r.err != nil {
		return notifications.SendResult{}, r.err
	}
	return notifications.SendResult{ProviderRef: r.ref}, nil
}

func TestNewIdentityDeliveryResolverComposesIdentity(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 32)
	cfg := config.Config{
		MaterialKeys: config.MaterialKeys{
			ActiveID: "v1",
			Keys:     map[string][]byte{"v1": key},
		},
	}
	got, err := newIdentityDeliveryResolver(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(*identityMaterialResolver); !ok {
		t.Fatalf("resolver type = %T", got)
	}
}

func mustNotifyAsIdentity(t *testing.T) identity.ID {
	t.Helper()
	nid, err := notifications.NewID()
	if err != nil {
		t.Fatal(err)
	}
	var id identity.ID
	copy(id[:], nid[:])
	return id
}

type stubIdentitySource struct {
	byID map[identity.ID]identity.VerificationDelivery
	err  error
}

func (s *stubIdentitySource) ResolveVerificationDelivery(_ context.Context, id identity.ID) (identity.VerificationDelivery, error) {
	if s.err != nil {
		return identity.VerificationDelivery{}, s.err
	}
	got, ok := s.byID[id]
	if !ok {
		return identity.VerificationDelivery{}, identity.ErrInvalidChallenge
	}
	return got, nil
}
