package needs

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/needs/contracts"
)

func TestFulfillFromCompletedTransactionOutcomes(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	created, err := svc.Create(context.Background(), owner, validContent("Need", ""))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	opened, err := svc.Open(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	txnID := mustID(t)
	got, err := svc.FulfillFromCompletedTransaction(context.Background(), contracts.TransactionFulfillmentCommand{
		NeedID:          contracts.ID(opened.ID),
		RequesterUserID: contracts.ID(owner),
		TransactionID:   contracts.ID(txnID),
	})
	if err != nil || got.Outcome != contracts.FulfillmentFulfilled || got.Status != string(StatusFulfilled) {
		t.Fatalf("fulfill = %+v err=%v", got, err)
	}
	again, err := svc.FulfillFromCompletedTransaction(context.Background(), contracts.TransactionFulfillmentCommand{
		NeedID:          contracts.ID(opened.ID),
		RequesterUserID: contracts.ID(owner),
		TransactionID:   contracts.ID(txnID),
	})
	if err != nil || again.Outcome != contracts.FulfillmentAlreadyFulfilled || again.Status != string(StatusFulfilled) {
		t.Fatalf("already = %+v err=%v", again, err)
	}
}

func TestOwnerFulfillStillRejectsAlreadyFulfilled(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	created, err := svc.Create(context.Background(), owner, validContent("Need", ""))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	opened, err := svc.Open(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Fulfill(context.Background(), owner, opened.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Fulfill(context.Background(), owner, opened.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("owner double fulfill err = %v", err)
	}
}

func TestFulfillFromCompletedTransactionDoesNotResurrect(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	created, err := svc.Create(context.Background(), owner, validContent("Need", ""))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	opened, err := svc.Open(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	cancelled, err := svc.Cancel(context.Background(), owner, opened.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.FulfillFromCompletedTransaction(context.Background(), contracts.TransactionFulfillmentCommand{
		NeedID:          contracts.ID(cancelled.ID),
		RequesterUserID: contracts.ID(owner),
		TransactionID:   contracts.ID(mustID(t)),
	})
	if err != nil || got.Outcome != contracts.FulfillmentNotFulfillable || got.Status != string(StatusCancelled) {
		t.Fatalf("cancelled = %+v err=%v", got, err)
	}
	stored, err := svc.GetOwned(context.Background(), owner, cancelled.ID)
	if err != nil || stored.Status != StatusCancelled || stored.UpdatedAt != cancelled.UpdatedAt {
		t.Fatalf("rewritten = %+v err=%v", stored, err)
	}

	expires := now.now.Add(time.Hour)
	content := validContent("Expire", "")
	content.ExpiresAt = &expires
	expNeed, err := svc.Create(context.Background(), owner, content)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Open(context.Background(), owner, expNeed.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(2 * time.Hour)
	expired, err := svc.ExpireIfDue(context.Background(), expNeed.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err = svc.FulfillFromCompletedTransaction(context.Background(), contracts.TransactionFulfillmentCommand{
		NeedID:          contracts.ID(expired.ID),
		RequesterUserID: contracts.ID(owner),
		TransactionID:   contracts.ID(mustID(t)),
	})
	if err != nil || got.Outcome != contracts.FulfillmentNotFulfillable || got.Status != string(StatusExpired) {
		t.Fatalf("expired = %+v err=%v", got, err)
	}
}

func TestFulfillFromCompletedTransactionRejectsUnrelatedRequester(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	other := mustID(t)
	created, err := svc.Create(context.Background(), owner, validContent("Need", ""))
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	opened, err := svc.Open(context.Background(), owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.FulfillFromCompletedTransaction(context.Background(), contracts.TransactionFulfillmentCommand{
		NeedID:          contracts.ID(opened.ID),
		RequesterUserID: contracts.ID(other),
		TransactionID:   contracts.ID(mustID(t)),
	})
	if err != nil || got.Outcome != contracts.FulfillmentNotFulfillable {
		t.Fatalf("unrelated = %+v err=%v", got, err)
	}
	stored, err := svc.GetOwned(context.Background(), owner, opened.ID)
	if err != nil || stored.Status != StatusOpen {
		t.Fatalf("need = %+v err=%v", stored, err)
	}
}
