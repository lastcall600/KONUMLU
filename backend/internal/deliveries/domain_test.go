package deliveries

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreatePendingAndSimpleLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	d, err := CreatePending(validCreate(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != StatusPending || d.Eligibility != EligibilityRequired || d.DispatchedAt != nil {
		t.Fatalf("d = %+v", d)
	}
	if _, _, err := d.MarkInTransit(now.Add(time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("pending in-transit err = %v", err)
	}
	if _, _, err := d.MarkDelivered(now.Add(time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("pending delivered err = %v", err)
	}
	ready, changed, err := d.MarkReady(now.Add(time.Minute))
	if err != nil || !changed || ready.Status != StatusReady {
		t.Fatalf("ready = %+v changed=%v err=%v", ready, changed, err)
	}
	again, changed, err := ready.MarkReady(now.Add(2 * time.Minute))
	if err != nil || changed || again.Status != StatusReady {
		t.Fatalf("idempotent ready = %+v changed=%v err=%v", again, changed, err)
	}
	if _, _, err := ready.MarkDelivered(now.Add(2 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("unset method must not skip in_transit err = %v", err)
	}
	transit, changed, err := ready.MarkInTransit(now.Add(3 * time.Minute))
	if err != nil || !changed || transit.Status != StatusInTransit || transit.DispatchedAt == nil {
		t.Fatalf("in-transit = %+v changed=%v err=%v", transit, changed, err)
	}
	delivered, changed, err := transit.MarkDelivered(now.Add(4 * time.Minute))
	if err != nil || !changed || delivered.Status != StatusDelivered || delivered.DeliveredAt == nil {
		t.Fatalf("delivered = %+v changed=%v err=%v", delivered, changed, err)
	}
	if _, _, err := delivered.Cancel(now.Add(5 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("delivered cancel err = %v", err)
	}
	same, changed, err := delivered.MarkDelivered(now.Add(5 * time.Minute))
	if err != nil || changed || same.Status != StatusDelivered {
		t.Fatalf("idempotent delivered = %+v changed=%v err=%v", same, changed, err)
	}
}

func TestHandoffAndPickupSkipInTransit(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	for _, method := range []Method{MethodHandoff, MethodPickup} {
		cmd := validCreate(t)
		cmd.Method = &method
		d, err := CreatePending(cmd, now)
		if err != nil {
			t.Fatal(err)
		}
		ready, _, err := d.MarkReady(now.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := ready.MarkInTransit(now.Add(2 * time.Minute)); !errors.Is(err, errInvalidTransition) {
			t.Fatalf("%s in-transit err = %v", method, err)
		}
		delivered, changed, err := ready.MarkDelivered(now.Add(2 * time.Minute))
		if err != nil || !changed || delivered.Status != StatusDelivered || delivered.DispatchedAt != nil {
			t.Fatalf("%s delivered = %+v err=%v", method, delivered, err)
		}
	}
}

func TestCourierRequiresInTransit(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	cmd := validCreate(t)
	m := MethodCourier
	cmd.Method = &m
	d, err := CreatePending(cmd, now)
	if err != nil {
		t.Fatal(err)
	}
	ready, _, err := d.MarkReady(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ready.MarkDelivered(now.Add(2 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("courier skip in-transit err = %v", err)
	}
}

func TestCancelFromOpenStates(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	d, err := CreatePending(validCreate(t), now)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, changed, err := d.Cancel(now.Add(time.Minute))
	if err != nil || !changed || cancelled.Status != StatusCancelled || cancelled.CancelledAt == nil {
		t.Fatalf("cancel pending = %+v err=%v", cancelled, err)
	}
	same, changed, err := cancelled.Cancel(now.Add(2 * time.Minute))
	if err != nil || changed {
		t.Fatalf("idempotent cancel err=%v changed=%v", err, changed)
	}
	_ = same
	if _, _, err := cancelled.MarkReady(now.Add(3 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("terminal ready err = %v", err)
	}
}

func TestMethodAndNoteAndEligibilityValidation(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	cmd := validCreate(t)
	bad := Method("drone")
	cmd.Method = &bad
	if _, err := CreatePending(cmd, now); !errors.Is(err, errInvalidMethod) {
		t.Fatalf("method err = %v", err)
	}
	cmd = validCreate(t)
	note := strings.Repeat("n", MaxNoteRunes+1)
	cmd.Note = &note
	if _, err := CreatePending(cmd, now); !errors.Is(err, errInvalidNote) {
		t.Fatalf("note err = %v", err)
	}
	cmd = validCreate(t)
	cmd.Eligibility = EligibilityNone
	if _, err := CreatePending(cmd, now); !errors.Is(err, errNotEligible) {
		t.Fatalf("none eligibility err = %v", err)
	}
	cmd = validCreate(t)
	cmd.ProviderUserID = cmd.RequesterUserID
	if _, err := CreatePending(cmd, now); !errors.Is(err, errInvalidDelivery) {
		t.Fatalf("same party err = %v", err)
	}
}

func validCreate(t *testing.T) CreateCommand {
	t.Helper()
	return CreateCommand{
		TransactionID:   mustID(t),
		RequesterUserID: mustID(t),
		ProviderUserID:  mustID(t),
		Eligibility:     EligibilityRequired,
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
