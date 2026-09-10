package transactions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"backend/internal/needs"
	needcontracts "backend/internal/needs/contracts"
	"backend/internal/platform/outbox"
	txncontracts "backend/internal/transactions/contracts"
)

func TestCompleteOpenNeedKeepsOutboxFulfillmentPath(t *testing.T) {
	env := mustFulfillEnv(t)
	txn := env.completeActive(t)
	need, err := env.needs.GetOwned(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID))
	if err != nil || need.Status != needs.StatusOpen {
		t.Fatalf("need after complete = %+v err=%v", need, err)
	}
	if err := env.handler.Handle(context.Background(), env.mustEvent(t, txn)); err != nil {
		t.Fatal(err)
	}
	fulfilled, err := env.needs.GetOwned(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID))
	if err != nil || fulfilled.Status != needs.StatusFulfilled {
		t.Fatalf("worker fulfill = %+v err=%v", fulfilled, err)
	}
}

func TestCompleteAlreadyFulfilledNeedIsExplicit(t *testing.T) {
	env := mustFulfillEnv(t)
	txn, err := env.svc.CreateFromAcceptedOffer(context.Background(), env.requester, env.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Start(context.Background(), env.requester, txn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.needs.Fulfill(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID)); err != nil {
		t.Fatal(err)
	}
	env.setNeedStatus(string(needs.StatusFulfilled))
	completed, err := env.svc.Complete(context.Background(), env.requester, txn.ID)
	if err != nil || completed.Status != StatusCompleted {
		t.Fatalf("complete = %+v err=%v", completed, err)
	}
	if err := env.handler.Handle(context.Background(), env.mustEvent(t, completed)); err != nil {
		t.Fatal(err)
	}
	need, err := env.needs.GetOwned(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID))
	if err != nil || need.Status != needs.StatusFulfilled {
		t.Fatalf("need = %+v err=%v", need, err)
	}
}

func TestCompleteEnqueuesIdempotentCompletedEvent(t *testing.T) {
	svc, fx, enq := mustTxnServiceWithOutbox(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), fx.requester, txn.ID); err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != 0 {
		t.Fatalf("start events = %+v", enq.events)
	}
	completed, err := svc.Complete(context.Background(), fx.requester, txn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != 1 || enq.events[0].EventType != txncontracts.EventTypeCompleted {
		t.Fatalf("complete events = %+v", enq.events)
	}
	payload, err := txncontracts.DecodeCompleted(enq.events[0].Payload)
	if err != nil || payload.NeedID != fx.needID.String() || payload.RequesterUserID != fx.requester.String() {
		t.Fatalf("payload = %+v err=%v", payload, err)
	}
	if _, err := svc.Complete(context.Background(), fx.requester, txn.ID); err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != 1 {
		t.Fatalf("replay events = %d", len(enq.events))
	}
	if completed.Status != StatusCompleted {
		t.Fatalf("status = %s", completed.Status)
	}
}

func TestCancelAndIncompleteDoNotEnqueueCompleted(t *testing.T) {
	svc, fx, enq := mustTxnServiceWithOutbox(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel(context.Background(), fx.requester, txn.ID); err != nil {
		t.Fatal(err)
	}
	if len(enq.events) != 0 {
		t.Fatalf("cancel events = %+v", enq.events)
	}
}

func TestCompletionHandlerFulfillsSourceNeedAndReplay(t *testing.T) {
	env := mustFulfillEnv(t)
	txn := env.completeActive(t)
	if err := env.handler.Handle(context.Background(), env.mustEvent(t, txn)); err != nil {
		t.Fatal(err)
	}
	need, err := env.needs.GetOwned(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID))
	if err != nil || need.Status != needs.StatusFulfilled {
		t.Fatalf("need = %+v err=%v", need, err)
	}
	if err := env.handler.Handle(context.Background(), env.mustEvent(t, txn)); err != nil {
		t.Fatal(err)
	}
	again, err := env.needs.GetOwned(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID))
	if err != nil || again.Status != needs.StatusFulfilled || again.UpdatedAt != need.UpdatedAt {
		t.Fatalf("replay = %+v err=%v", again, err)
	}
}

func TestCompletionHandlerAlreadyFulfilledIsSafe(t *testing.T) {
	env := mustFulfillEnv(t)
	txn := env.completeActive(t)
	if _, err := env.needs.Fulfill(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID)); err != nil {
		t.Fatal(err)
	}
	if err := env.handler.Handle(context.Background(), env.mustEvent(t, txn)); err != nil {
		t.Fatal(err)
	}
	need, err := env.needs.GetOwned(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID))
	if err != nil || need.Status != needs.StatusFulfilled {
		t.Fatalf("need = %+v err=%v", need, err)
	}
}

func TestCompletionHandlerCancelledAndExpiredAreExplicit(t *testing.T) {
	env := mustFulfillEnv(t)
	txn := env.completeActive(t)
	if _, err := env.needs.Cancel(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID)); err != nil {
		t.Fatal(err)
	}
	if err := env.handler.Handle(context.Background(), env.mustEvent(t, txn)); err != nil {
		t.Fatal(err)
	}
	need, err := env.needs.GetOwned(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID))
	if err != nil || need.Status != needs.StatusCancelled {
		t.Fatalf("cancelled rewritten = %+v err=%v", need, err)
	}
	got, err := env.store.Get(context.Background(), txn.ID)
	if err != nil || got.Status != StatusCompleted {
		t.Fatalf("txn = %+v err=%v", got, err)
	}
}

func TestFailedFulfillmentDoesNotCorruptTransaction(t *testing.T) {
	svc, fx, _ := mustTxnServiceWithOutbox(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), fx.requester, txn.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.Complete(context.Background(), fx.requester, txn.ID)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewCompletionHandler(svc.store, failingFulfillment{err: needcontracts.ErrUnavailable})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), mustCompletedEvent(t, completed)); !errors.Is(err, needcontracts.ErrUnavailable) {
		t.Fatalf("handler err = %v", err)
	}
	got, err := svc.Get(context.Background(), fx.requester, txn.ID)
	if err != nil || got.Status != StatusCompleted || got.CompletedAt == nil {
		t.Fatalf("txn corrupted = %+v err=%v", got, err)
	}
}

func TestHandlerDoesNotFulfillUnrelatedNeed(t *testing.T) {
	env := mustFulfillEnv(t)
	other, err := env.needs.Create(context.Background(), needs.ID(txnIDFrom(env.requester)), needs.Content{
		Title:    "Other",
		Location: needs.Coordinates{Latitude: 36.621, Longitude: 29.116},
	})
	if err != nil {
		t.Fatal(err)
	}
	env.clock.now = env.clock.now.Add(time.Minute)
	opened, err := env.needs.Open(context.Background(), needs.ID(txnIDFrom(env.requester)), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	txn := env.completeActive(t)
	if err := env.handler.Handle(context.Background(), env.mustEvent(t, txn)); err != nil {
		t.Fatal(err)
	}
	source, err := env.needs.GetOwned(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID))
	if err != nil || source.Status != needs.StatusFulfilled {
		t.Fatalf("source = %+v err=%v", source, err)
	}
	otherGot, err := env.needs.GetOwned(context.Background(), needs.ID(txn.RequesterUserID), opened.ID)
	if err != nil || otherGot.Status != needs.StatusOpen {
		t.Fatalf("unrelated = %+v err=%v", otherGot, err)
	}
}

func TestHandlerSkipsWhenTransactionNotCompleted(t *testing.T) {
	env := mustFulfillEnv(t)
	txn, err := env.svc.CreateFromAcceptedOffer(context.Background(), env.requester, env.offerID)
	if err != nil {
		t.Fatal(err)
	}
	active, err := env.svc.Start(context.Background(), env.requester, txn.ID)
	if err != nil {
		t.Fatal(err)
	}
	ev := env.mustEvent(t, completedCopy(active))
	if err := env.handler.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	need, err := env.needs.GetOwned(context.Background(), needs.ID(txn.RequesterUserID), needs.ID(txn.NeedID))
	if err != nil || need.Status != needs.StatusOpen {
		t.Fatalf("need = %+v err=%v", need, err)
	}
}

func mustTxnServiceWithOutbox(t *testing.T) (*Service, *txnFixture, *memoryEnqueuer) {
	t.Helper()
	svc, fx := mustTxnService(t)
	enq := &memoryEnqueuer{}
	svc.SetOutbox(nil, enq)
	return svc, fx, enq
}

type fulfillEnv struct {
	svc       *Service
	store     *MemoryStore
	needs     *needs.Service
	handler   *CompletionHandler
	clock     *frozenNow
	requester ID
	offerID   ID
	needID    ID
	fx        *txnFixture
}

func mustFulfillEnv(t *testing.T) *fulfillEnv {
	t.Helper()
	clock := &frozenNow{now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	needStore := needs.NewMemoryStore()
	needSvc, err := needs.NewService(needStore, nil, nil, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	requester, err := needs.NewID()
	if err != nil {
		t.Fatal(err)
	}
	created, err := needSvc.Create(context.Background(), requester, needs.Content{
		Title:    "Need",
		Location: needs.Coordinates{Latitude: 36.621, Longitude: 29.116},
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Minute)
	opened, err := needSvc.Open(context.Background(), requester, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	fx := &txnFixture{
		offerID:     mustID(t),
		needID:      ID(opened.ID),
		requester:   ID(requester),
		provider:    mustID(t),
		businessID:  mustID(t),
		serviceID:   mustID(t),
		offerStatus: "accepted",
		needStatus:  "open",
	}
	store := NewMemoryStore()
	svc, err := NewService(store, stubOffers{fx: fx}, stubNeeds{fx: fx}, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	enq := &memoryEnqueuer{}
	svc.SetOutbox(nil, enq)
	h, err := NewCompletionHandler(store, needSvc)
	if err != nil {
		t.Fatal(err)
	}
	return &fulfillEnv{
		svc:       svc,
		store:     store,
		needs:     needSvc,
		handler:   h,
		clock:     clock,
		requester: ID(requester),
		offerID:   fx.offerID,
		needID:    ID(opened.ID),
		fx:        fx,
	}
}

func (e *fulfillEnv) setNeedStatus(status string) {
	e.fx.needStatus = status
}

func (e *fulfillEnv) completeActive(t *testing.T) Transaction {
	t.Helper()
	txn, err := e.svc.CreateFromAcceptedOffer(context.Background(), e.requester, e.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Start(context.Background(), e.requester, txn.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := e.svc.Complete(context.Background(), e.requester, txn.ID)
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

func (e *fulfillEnv) mustEvent(t *testing.T, txn Transaction) outbox.Event {
	t.Helper()
	return mustCompletedEvent(t, txn)
}

func mustCompletedEvent(t *testing.T, txn Transaction) outbox.Event {
	t.Helper()
	in, err := encodeCompletedEvent(txn)
	if err != nil {
		t.Fatal(err)
	}
	id, err := outbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return outbox.Event{
		ID:           id,
		EventType:    in.EventType,
		EventVersion: in.EventVersion,
		Payload:      in.Payload,
	}
}

func completedCopy(txn Transaction) Transaction {
	stamp := txn.UpdatedAt.UTC()
	txn.Status = StatusCompleted
	txn.CompletedAt = &stamp
	return txn
}

func txnIDFrom(id ID) needs.ID {
	return needs.ID(id)
}

type failingFulfillment struct {
	err error
}

func (f failingFulfillment) FulfillFromCompletedTransaction(context.Context, needcontracts.TransactionFulfillmentCommand) (needcontracts.FulfillmentResult, error) {
	return needcontracts.FulfillmentResult{}, f.err
}

type memoryEnqueuer struct {
	events []outbox.NewEvent
}

func (e *memoryEnqueuer) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	e.events = append(e.events, in)
	return outbox.Event{ID: id, EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
}

func TestDecodeCompletedRejectsEmpty(t *testing.T) {
	if _, err := txncontracts.DecodeCompleted(json.RawMessage(`{}`)); !errors.Is(err, txncontracts.ErrInvalidEvent) {
		t.Fatalf("err = %v", err)
	}
}
