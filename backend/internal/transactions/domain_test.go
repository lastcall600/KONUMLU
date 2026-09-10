package transactions

import (
	"errors"
	"testing"
	"time"
)

func TestCreatePendingAndLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	txn, err := CreatePending(validSource(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if txn.Status != StatusPending || txn.CompletedAt != nil || txn.CancelledAt != nil {
		t.Fatalf("txn = %+v", txn)
	}
	if _, _, err := txn.Complete(now.Add(time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("pending complete err = %v", err)
	}
	active, changed, err := txn.Start(now.Add(time.Minute))
	if err != nil || !changed || active.Status != StatusActive {
		t.Fatalf("start = %+v changed=%v err=%v", active, changed, err)
	}
	again, changed, err := active.Start(now.Add(2 * time.Minute))
	if err != nil || changed || again.Status != StatusActive {
		t.Fatalf("idempotent start = %+v changed=%v err=%v", again, changed, err)
	}
	done, changed, err := active.Complete(now.Add(3 * time.Minute))
	if err != nil || !changed || done.Status != StatusCompleted || done.CompletedAt == nil {
		t.Fatalf("complete = %+v changed=%v err=%v", done, changed, err)
	}
	if _, _, err := done.Cancel(now.Add(4 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("completed cancel err = %v", err)
	}
	if _, _, err := done.Start(now.Add(4 * time.Minute)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
}

func TestCancelFromPendingAndActive(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	txn, err := CreatePending(validSource(t), now)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, changed, err := txn.Cancel(now.Add(time.Minute))
	if err != nil || !changed || cancelled.Status != StatusCancelled || cancelled.CancelledAt == nil {
		t.Fatalf("cancel pending = %+v err=%v", cancelled, err)
	}
	same, changed, err := cancelled.Cancel(now.Add(2 * time.Minute))
	if err != nil || changed {
		t.Fatalf("idempotent cancel err=%v changed=%v", err, changed)
	}
	_ = same
	active, _, err := txn.Start(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, changed, err = active.Cancel(now.Add(2 * time.Minute))
	if err != nil || !changed || cancelled.Status != StatusCancelled {
		t.Fatalf("cancel active = %+v err=%v", cancelled, err)
	}
}

func TestPriceCopiedAndRejected(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	src := validSource(t)
	src.Price = &Price{Amount: "250.50", Currency: "TRY"}
	txn, err := CreatePending(src, now)
	if err != nil || txn.Price == nil || txn.Price.Amount != "250.50" {
		t.Fatalf("txn = %+v err=%v", txn, err)
	}
	src.Price = &Price{Amount: "10", Currency: ""}
	if _, err := CreatePending(src, now); !errors.Is(err, errInvalidPrice) {
		t.Fatalf("price err = %v", err)
	}
}

func TestCreateRejectsSameParty(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	src := validSource(t)
	src.ProviderUserID = src.RequesterUserID
	if _, err := CreatePending(src, now); !errors.Is(err, errInvalidTxn) {
		t.Fatalf("err = %v", err)
	}
}

func validSource(t *testing.T) AcceptedSource {
	t.Helper()
	return AcceptedSource{
		OfferID:            mustID(t),
		NeedID:             mustID(t),
		RequesterUserID:    mustID(t),
		ProviderUserID:     mustID(t),
		ProviderBusinessID: mustID(t),
		ServiceID:          mustID(t),
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
