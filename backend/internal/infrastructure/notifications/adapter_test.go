package notifications

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	domain "backend/internal/notifications"
)

func TestBindDisabledLeavesSendersNil(t *testing.T) {
	got, err := Bind(modeDisabled, modeDisabled, Transports{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != nil || got.SMS != nil {
		t.Fatal("disabled channels must not register a sender")
	}
}

func TestBindExternalWithoutAdapterFails(t *testing.T) {
	_, err := Bind(modeExternal, modeDisabled, Transports{})
	if !errors.Is(err, errEmailAdapterRequired) {
		t.Fatalf("email err = %v", err)
	}
	_, err = Bind(modeDisabled, modeExternal, Transports{})
	if !errors.Is(err, errSMSAdapterRequired) {
		t.Fatalf("sms err = %v", err)
	}
}

func TestBindExternalWithAdapterWiresPorts(t *testing.T) {
	got, err := Bind(modeExternal, modeExternal, Transports{
		Email: &recordingEmail{ref: "e1"},
		SMS:   &recordingSMS{ref: "s1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Email == nil || got.SMS == nil {
		t.Fatal("external mode must wire adapters")
	}
	if _, ok := got.Email.(*EmailAdapter); !ok {
		t.Fatalf("email type = %T", got.Email)
	}
	if _, ok := got.SMS.(*SMSAdapter); !ok {
		t.Fatalf("sms type = %T", got.SMS)
	}
}

func TestEmailAdapterReceivesIdempotencyKey(t *testing.T) {
	rec := &recordingEmail{ref: "ref-1"}
	a, err := NewEmailAdapter(rec)
	if err != nil {
		t.Fatal(err)
	}
	key := "stable-delivery-id"
	_, err = a.Send(context.Background(), domain.EmailSendRequest{
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

func TestSMSAdapterReceivesIdempotencyKey(t *testing.T) {
	rec := &recordingSMS{ref: "sms-ref"}
	a, err := NewSMSAdapter(rec)
	if err != nil {
		t.Fatal(err)
	}
	key := "stable-delivery-id"
	_, err = a.Send(context.Background(), domain.SMSSendRequest{
		Destination:        "+15551234567",
		TemplateCode:       "identity.verification.signup",
		TemplateVersion:    1,
		Locale:             "tr",
		VerificationSecret: "123456",
		IdempotencyKey:     key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.last.IdempotencyKey != key {
		t.Fatalf("idempotency = %q", rec.last.IdempotencyKey)
	}
}

func TestAdapterProviderErrorMapsRetryablyWithoutLeak(t *testing.T) {
	dest := "owner@example.com"
	secret := "otp-or-token"
	rec := &recordingEmail{err: fmt.Errorf("vendor 503 dest=%s token=%s body={\"raw\":true}", dest, secret)}
	a, err := NewEmailAdapter(rec)
	if err != nil {
		t.Fatal(err)
	}
	_, got := a.Send(context.Background(), domain.EmailSendRequest{IdempotencyKey: "k"})
	if !errors.Is(got, domain.ErrUnavailable) {
		t.Fatalf("err = %v", got)
	}
	msg := got.Error()
	if strings.Contains(msg, dest) || strings.Contains(msg, secret) || strings.Contains(msg, "vendor 503") {
		t.Fatalf("leaked provider detail: %v", got)
	}
}

func TestBindHasNoProductionNoopSender(t *testing.T) {
	got, err := Bind(modeDisabled, modeDisabled, Transports{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != nil || got.SMS != nil {
		t.Fatal("production bind must not substitute a noop sender")
	}
}

type recordingEmail struct {
	last domain.EmailSendRequest
	ref  string
	err  error
}

func (r *recordingEmail) Send(_ context.Context, req domain.EmailSendRequest) (domain.SendResult, error) {
	r.last = req
	if r.err != nil {
		return domain.SendResult{}, r.err
	}
	return domain.SendResult{ProviderRef: r.ref}, nil
}

type recordingSMS struct {
	last domain.SMSSendRequest
	ref  string
	err  error
}

func (r *recordingSMS) Send(_ context.Context, req domain.SMSSendRequest) (domain.SendResult, error) {
	r.last = req
	if r.err != nil {
		return domain.SendResult{}, r.err
	}
	return domain.SendResult{ProviderRef: r.ref}, nil
}
