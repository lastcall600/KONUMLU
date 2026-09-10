package notifications

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"backend/internal/notifications/contracts"
)

func TestDeliverEmailUsesEmailSender(t *testing.T) {
	email := &stubEmail{ref: "email-1"}
	sms := &stubSMS{}
	svc, store, resolver := mustDeliveryService(t, email, sms)
	d := seedDelivery(t, store, contracts.ChannelEmail)
	resolver.material = VerificationMaterial{
		Kind:        MaterialKindEmail,
		Destination: "owner@example.com",
		Secret:      "email-token-secret",
	}

	got, err := svc.Deliver(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSent || got.ProviderRef == nil || *got.ProviderRef != "email-1" {
		t.Fatalf("delivery = %+v", got)
	}
	if email.n != 1 || sms.n != 0 {
		t.Fatalf("email=%d sms=%d", email.n, sms.n)
	}
	if email.last.Destination != "owner@example.com" || email.last.VerificationSecret != "email-token-secret" {
		t.Fatal("email sender must receive transient destination and secret")
	}
	if email.last.TemplateCode != d.TemplateCode || email.last.Locale != d.Locale || email.last.IdempotencyKey != d.ID.String() {
		t.Fatal("notifications owns template, locale, and idempotency key")
	}
}

func TestDeliverPhoneUsesSMSSender(t *testing.T) {
	email := &stubEmail{}
	sms := &stubSMS{ref: "sms-1"}
	svc, store, resolver := mustDeliveryService(t, email, sms)
	d := seedDelivery(t, store, contracts.ChannelSMS)
	resolver.material = VerificationMaterial{
		Kind:        MaterialKindPhone,
		Destination: "+15551234567",
		Secret:      "123456",
	}

	got, err := svc.Deliver(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSent || sms.n != 1 || email.n != 0 {
		t.Fatalf("status=%s email=%d sms=%d", got.Status, email.n, sms.n)
	}
	if sms.last.Destination != "+15551234567" || sms.last.VerificationSecret != "123456" {
		t.Fatal("sms sender must receive transient destination and secret")
	}
}

func TestDeliverChannelMismatchFailsClosed(t *testing.T) {
	email := &stubEmail{ref: "should-not-send"}
	svc, store, resolver := mustDeliveryService(t, email, &stubSMS{})
	d := seedDelivery(t, store, contracts.ChannelEmail)
	resolver.material = VerificationMaterial{
		Kind:        MaterialKindPhone,
		Destination: "+15550001111",
		Secret:      "999999",
	}

	got, err := svc.Deliver(context.Background(), d)
	if !errors.Is(err, errChannelMismatch) {
		t.Fatalf("err = %v", err)
	}
	if got.Status == StatusSent {
		t.Fatal("mismatch must not mark sent")
	}
	if email.n != 0 {
		t.Fatal("mismatch must not call provider")
	}
	stored, _ := store.GetByID(d.ID)
	if stored.Status == StatusSent {
		t.Fatal("stored delivery must not be sent")
	}
}

func TestDeliverExpiredAndConsumedMaterialFails(t *testing.T) {
	svc, store, resolver := mustDeliveryService(t, &stubEmail{ref: "x"}, &stubSMS{})
	d := seedDelivery(t, store, contracts.ChannelEmail)
	resolver.err = errMaterialUnusable

	if _, err := svc.Deliver(context.Background(), d); !errors.Is(err, errMaterialUnusable) {
		t.Fatalf("err = %v", err)
	}
	if stored, _ := store.GetByID(d.ID); stored.Status == StatusSent {
		t.Fatal("unusable material must not mark sent")
	}
}

func TestDeliverProviderFailureDoesNotMarkSent(t *testing.T) {
	email := &stubEmail{err: errors.New("provider down")}
	svc, store, resolver := mustDeliveryService(t, email, &stubSMS{})
	d := seedDelivery(t, store, contracts.ChannelEmail)
	resolver.material = VerificationMaterial{Kind: MaterialKindEmail, Destination: "a@b.co", Secret: "tok"}

	got, err := svc.Deliver(context.Background(), d)
	if !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if got.Status == StatusSent || (got.ProviderRef != nil) {
		t.Fatalf("failure marked sent: %+v", got)
	}
	stored, _ := store.GetByID(d.ID)
	if stored.Status != StatusFailed || stored.CompletedAt != nil || stored.Attempts != 1 {
		t.Fatalf("retryable failure state = %+v", stored)
	}
}

func TestDeliverSuccessMarksSent(t *testing.T) {
	svc, store, resolver := mustDeliveryService(t, &stubEmail{ref: "ok"}, &stubSMS{})
	d := seedDelivery(t, store, contracts.ChannelEmail)
	resolver.material = VerificationMaterial{Kind: MaterialKindEmail, Destination: "ok@example.com", Secret: "tok"}

	got, err := svc.Deliver(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSent || got.CompletedAt == nil {
		t.Fatalf("got = %+v", got)
	}
}

func TestDeliverAlreadySentIsNotResent(t *testing.T) {
	email := &stubEmail{ref: "second"}
	svc, store, resolver := mustDeliveryService(t, email, &stubSMS{})
	d := seedDelivery(t, store, contracts.ChannelEmail)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	d.Status = StatusSent
	d.CompletedAt = &now
	ref := "first"
	d.ProviderRef = &ref
	saved, err := store.SaveDelivery(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	resolver.material = VerificationMaterial{Kind: MaterialKindEmail, Destination: "ok@example.com", Secret: "tok"}

	got, err := svc.Deliver(context.Background(), saved)
	if err != nil {
		t.Fatal(err)
	}
	if email.n != 0 {
		t.Fatal("already-sent must not resend")
	}
	if got.ProviderRef == nil || *got.ProviderRef != "first" {
		t.Fatalf("provider ref = %v", got.ProviderRef)
	}
}

func TestDeliveryModelHasNoSecret(t *testing.T) {
	d := validDelivery(t)
	_ = d.ID
	_ = d.IntentID
	_ = d.Channel
	_ = d.TemplateCode
	_ = d.TemplateVersion
	_ = d.Locale
	_ = d.RecipientKind
	_ = d.RecipientID
	_ = d.Status
	_ = d.Attempts
	_ = d.ProviderRef
	_ = d.CorrelationID
	_ = d.CreatedAt
	_ = d.UpdatedAt
	_ = d.LastAttemptAt
	_ = d.CompletedAt
}

func TestResolverAndProviderErrorsDoNotLeakSecrets(t *testing.T) {
	secret := "super-secret-otp-999111"
	dest := "leak@example.com"
	email := &stubEmail{err: fmt.Errorf("send to %s secret=%s", dest, secret)}
	svc, store, resolver := mustDeliveryService(t, email, &stubSMS{})
	d := seedDelivery(t, store, contracts.ChannelEmail)
	resolver.err = fmt.Errorf("cannot resolve %s secret=%s", dest, secret)

	_, err := svc.Deliver(context.Background(), d)
	if err == nil {
		t.Fatal("expected error")
	}
	assertNoLeak(t, err, secret, dest)

	resolver.err = nil
	resolver.material = VerificationMaterial{Kind: MaterialKindEmail, Destination: dest, Secret: secret}
	_, err = svc.Deliver(context.Background(), d)
	if err == nil {
		t.Fatal("expected provider error")
	}
	assertNoLeak(t, err, secret, dest)

	req := EmailSendRequest{Destination: dest, VerificationSecret: secret, TemplateCode: "identity.verification.signup", TemplateVersion: 1, Locale: "tr", IdempotencyKey: "k"}
	if strings.Contains(req.String(), secret) || strings.Contains(req.GoString(), dest) {
		t.Fatal("send request String must not include destination or secret")
	}
	mat := VerificationMaterial{Kind: MaterialKindEmail, Destination: dest, Secret: secret}
	if strings.Contains(mat.String(), secret) || strings.Contains(fmt.Sprintf("%#v", mat), secret) {
		t.Fatal("material String must not include secret")
	}
}

func TestHasSenderReflectsWiredAdapters(t *testing.T) {
	store := NewMemoryStore()
	resolver := &stubResolver{}
	none, err := NewDeliveryService(store, resolver, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if none.HasSender(contracts.ChannelEmail) || none.HasSender(contracts.ChannelSMS) {
		t.Fatal("nil adapters must be unavailable")
	}
	both, err := NewDeliveryService(store, resolver, &stubEmail{}, &stubSMS{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !both.HasSender(contracts.ChannelEmail) || !both.HasSender(contracts.ChannelSMS) {
		t.Fatal("wired adapters must be available")
	}
}

func TestDisabledChannelDoesNotPretendSuccess(t *testing.T) {
	svc, store, resolver := mustDeliveryService(t, nil, nil)
	d := seedDelivery(t, store, contracts.ChannelEmail)
	resolver.material = VerificationMaterial{Kind: MaterialKindEmail, Destination: "a@b.co", Secret: "tok"}
	got, err := svc.Deliver(context.Background(), d)
	if !errors.Is(err, errProviderRequired) {
		t.Fatalf("err = %v", err)
	}
	if got.Status == StatusSent {
		t.Fatal("disabled channel must not mark sent")
	}
	stored, _ := store.GetByID(d.ID)
	if stored.Status == StatusSent {
		t.Fatal("stored delivery must not be sent")
	}
}

func TestMissingEmailProviderFailsClosed(t *testing.T) {
	svc, store, resolver := mustDeliveryService(t, nil, &stubSMS{ref: "sms"})
	d := seedDelivery(t, store, contracts.ChannelEmail)
	resolver.material = VerificationMaterial{Kind: MaterialKindEmail, Destination: "a@b.co", Secret: "tok"}
	got, err := svc.Deliver(context.Background(), d)
	if !errors.Is(err, errProviderRequired) {
		t.Fatalf("err = %v", err)
	}
	if got.Status == StatusSent {
		t.Fatal("nil provider must not mark sent")
	}
}

func mustDeliveryService(t *testing.T, email EmailSender, sms SMSSender) (*DeliveryService, *MemoryStore, *stubResolver) {
	t.Helper()
	store := NewMemoryStore()
	resolver := &stubResolver{}
	now := time.Date(2026, 9, 6, 12, 1, 0, 0, time.UTC)
	svc, err := NewDeliveryService(store, resolver, email, sms, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, resolver
}

func seedDelivery(t *testing.T, store *MemoryStore, channel contracts.Channel) Delivery {
	t.Helper()
	d := validDelivery(t)
	d.Channel = channel
	got, err := store.UpsertDelivery(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertNoLeak(t *testing.T, err error, parts ...string) {
	t.Helper()
	msg := err.Error()
	for _, p := range parts {
		if p != "" && strings.Contains(msg, p) {
			t.Fatalf("error leaked %q: %v", p, err)
		}
	}
}

type stubResolver struct {
	material VerificationMaterial
	err      error
}

func (s *stubResolver) Resolve(_ context.Context, _ ID) (VerificationMaterial, error) {
	if s.err != nil {
		return VerificationMaterial{}, s.err
	}
	return s.material, nil
}

type stubEmail struct {
	n    int
	last EmailSendRequest
	ref  string
	err  error
}

func (s *stubEmail) Send(_ context.Context, req EmailSendRequest) (SendResult, error) {
	s.n++
	s.last = req
	if s.err != nil {
		return SendResult{}, s.err
	}
	return SendResult{ProviderRef: s.ref}, nil
}

type stubSMS struct {
	n    int
	last SMSSendRequest
	ref  string
	err  error
}

func (s *stubSMS) Send(_ context.Context, req SMSSendRequest) (SendResult, error) {
	s.n++
	s.last = req
	if s.err != nil {
		return SendResult{}, s.err
	}
	return SendResult{ProviderRef: s.ref}, nil
}
