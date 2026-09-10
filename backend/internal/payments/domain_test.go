package payments

import (
	"errors"
	"testing"
	"time"
)

func TestCreatePendingAndLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	p, err := CreatePending(validCreate(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusPending || p.AuthorizedAt != nil || p.CapturedAt != nil {
		t.Fatalf("p = %+v", p)
	}
	if _, _, err := p.Capture(now.Add(time.Minute), ProviderSignal{}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("pending capture err = %v", err)
	}
	auth, changed, err := p.Authorize(now.Add(time.Minute), ProviderSignal{})
	if err != nil || !changed || auth.Status != StatusAuthorized || auth.AuthorizedAt == nil {
		t.Fatalf("authorize = %+v changed=%v err=%v", auth, changed, err)
	}
	again, changed, err := auth.Authorize(now.Add(2*time.Minute), ProviderSignal{})
	if err != nil || changed || again.Status != StatusAuthorized {
		t.Fatalf("idempotent authorize = %+v changed=%v err=%v", again, changed, err)
	}
	captured, changed, err := auth.Capture(now.Add(3*time.Minute), ProviderSignal{})
	if err != nil || !changed || captured.Status != StatusCaptured || captured.CapturedAt == nil {
		t.Fatalf("capture = %+v changed=%v err=%v", captured, changed, err)
	}
	if _, _, err := captured.Cancel(now.Add(4*time.Minute), ProviderSignal{}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("captured cancel err = %v", err)
	}
	if _, _, err := captured.Fail(now.Add(4*time.Minute), ProviderSignal{}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("captured fail err = %v", err)
	}
}

func TestCancelAndFailFromPendingAndAuthorized(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	p, err := CreatePending(validCreate(t), now)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, changed, err := p.Cancel(now.Add(time.Minute), ProviderSignal{})
	if err != nil || !changed || cancelled.Status != StatusCancelled || cancelled.CancelledAt == nil {
		t.Fatalf("cancel pending = %+v err=%v", cancelled, err)
	}
	same, changed, err := cancelled.Cancel(now.Add(2*time.Minute), ProviderSignal{})
	if err != nil || changed {
		t.Fatalf("idempotent cancel err=%v changed=%v", err, changed)
	}
	_ = same
	auth, _, err := p.Authorize(now.Add(time.Minute), ProviderSignal{})
	if err != nil {
		t.Fatal(err)
	}
	failed, changed, err := auth.Fail(now.Add(2*time.Minute), ProviderSignal{})
	if err != nil || !changed || failed.Status != StatusFailed || failed.FailedAt == nil {
		t.Fatalf("fail authorized = %+v err=%v", failed, err)
	}
}

func TestAmountRequiredAndSamePartyRejected(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	cmd := validCreate(t)
	cmd.Amount = Money{Amount: "10", Currency: ""}
	if _, err := CreatePending(cmd, now); !errors.Is(err, errInvalidAmount) {
		t.Fatalf("amount err = %v", err)
	}
	cmd = validCreate(t)
	cmd.PayeeUserID = cmd.PayerUserID
	if _, err := CreatePending(cmd, now); !errors.Is(err, errInvalidPayment) {
		t.Fatalf("same party err = %v", err)
	}
}

func validCreate(t *testing.T) CreateCommand {
	t.Helper()
	return CreateCommand{
		TransactionID: mustID(t),
		PayerUserID:   mustID(t),
		PayeeUserID:   mustID(t),
		Amount:        Money{Amount: "250.50", Currency: "TRY"},
	}
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
