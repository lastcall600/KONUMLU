package deliveries

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/deliveries/contracts"
	txncontracts "backend/internal/transactions/contracts"
)

func TestCreateFromEligibleTransactionDerivesParties(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != StatusPending || d.Eligibility != EligibilityRequired {
		t.Fatalf("d = %+v", d)
	}
	if d.RequesterUserID != fx.requester || d.ProviderUserID != fx.provider {
		t.Fatalf("parties = requester %s provider %s", d.RequesterUserID, d.ProviderUserID)
	}
	again, err := svc.CreateForTransaction(context.Background(), fx.provider, fx.txnID, CreateInput{
		Eligibility: EligibilityRequired,
		Method:      methodPtr(MethodCourier),
	})
	if err != nil || again.ID != d.ID || again.Method != nil {
		t.Fatalf("idempotent = %+v err=%v", again, err)
	}
}

func TestCreateDoesNotTouchPaymentsTrustOrNeeds(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	if _, ok := any(svc.store).(interface{ Payments() int }); ok {
		t.Fatal("store must not expose payments")
	}
	if _, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{}); err != nil {
		t.Fatal(err)
	}
	if fx.txns.calls.Load() != 1 {
		t.Fatalf("expected only Transactions Lookup, calls=%d", fx.txns.calls.Load())
	}
}

func TestCancelledCompletedAndNoneEligibilityRejected(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	fx.status = "cancelled"
	if _, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{}); !errors.Is(err, errNotEligible) {
		t.Fatalf("cancelled err = %v", err)
	}
	fx.status = "completed"
	if _, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{}); !errors.Is(err, errNotEligible) {
		t.Fatalf("completed err = %v", err)
	}
	fx.status = "pending"
	if _, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{Eligibility: EligibilityNone}); !errors.Is(err, errNotEligible) {
		t.Fatalf("explicit none err = %v", err)
	}
}

func TestCreateDerivesPartiesAndDeniesUnrelated(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	if _, err := svc.CreateForTransaction(context.Background(), mustID(t), fx.txnID, CreateInput{}); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger create err = %v", err)
	}
}

func TestParticipantReadAndUnrelatedNotFound(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), mustID(t), d.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger get err = %v", err)
	}
	got, err := svc.Get(context.Background(), fx.provider, d.ID)
	if err != nil || got.ID != d.ID {
		t.Fatalf("provider get = %+v err=%v", got, err)
	}
	listed, err := svc.ListMine(context.Background(), fx.requester)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %+v err=%v", listed, err)
	}
}

func TestProviderProgressionRejectedWhenTransactionCancelled(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	fx.status = "cancelled"
	if _, err := svc.MarkReady(context.Background(), fx.provider, d.ID); !errors.Is(err, errNotEligible) {
		t.Fatalf("ready on cancelled txn err = %v", err)
	}
	got, err := svc.Get(context.Background(), fx.provider, d.ID)
	if err != nil || got.Status != StatusPending {
		t.Fatalf("delivery mutated = %+v err=%v", got, err)
	}

	fx.status = "pending"
	ready, err := svc.MarkReady(context.Background(), fx.provider, d.ID)
	if err != nil || ready.Status != StatusReady {
		t.Fatalf("ready = %+v err=%v", ready, err)
	}
	fx.status = "cancelled"
	if _, err := svc.MarkInTransit(context.Background(), fx.provider, d.ID); !errors.Is(err, errNotEligible) {
		t.Fatalf("in-transit on cancelled txn err = %v", err)
	}
	fx.status = "pending"
	transit, err := svc.MarkInTransit(context.Background(), fx.provider, d.ID)
	if err != nil || transit.Status != StatusInTransit {
		t.Fatalf("in-transit = %+v err=%v", transit, err)
	}
	fx.status = "cancelled"
	if _, err := svc.MarkDelivered(context.Background(), fx.provider, d.ID); !errors.Is(err, errNotEligible) {
		t.Fatalf("delivered on cancelled txn err = %v", err)
	}
	cancelled, err := svc.Cancel(context.Background(), fx.requester, d.ID)
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("delivery cancel = %+v err=%v", cancelled, err)
	}
	same, err := svc.Cancel(context.Background(), fx.requester, d.ID)
	if err != nil || same.Status != StatusCancelled {
		t.Fatalf("terminal cancel = %+v err=%v", same, err)
	}
}

func TestProviderLifecycleAndRequesterCannotAdvance(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkReady(context.Background(), fx.requester, d.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("requester ready err = %v", err)
	}
	if _, err := svc.MarkInTransit(context.Background(), fx.requester, d.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("requester in-transit err = %v", err)
	}
	if _, err := svc.MarkDelivered(context.Background(), fx.requester, d.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("requester delivered err = %v", err)
	}
	if _, err := svc.MarkInTransit(context.Background(), fx.provider, d.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("pending in-transit err = %v", err)
	}
	ready, err := svc.MarkReady(context.Background(), fx.provider, d.ID)
	if err != nil || ready.Status != StatusReady {
		t.Fatalf("ready = %+v err=%v", ready, err)
	}
	same, err := svc.MarkReady(context.Background(), fx.provider, d.ID)
	if err != nil || same.Status != StatusReady {
		t.Fatalf("duplicate ready = %+v err=%v", same, err)
	}
	transit, err := svc.MarkInTransit(context.Background(), fx.provider, d.ID)
	if err != nil || transit.Status != StatusInTransit {
		t.Fatalf("in-transit = %+v err=%v", transit, err)
	}
	delivered, err := svc.MarkDelivered(context.Background(), fx.provider, d.ID)
	if err != nil || delivered.Status != StatusDelivered {
		t.Fatalf("delivered = %+v err=%v", delivered, err)
	}
	if _, err := svc.Cancel(context.Background(), fx.provider, d.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("delivered cancel err = %v", err)
	}
}

func TestHandoffSkipInTransitAndCancelByRequester(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{Method: methodPtr(MethodHandoff)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkReady(context.Background(), fx.provider, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkInTransit(context.Background(), fx.provider, d.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("handoff in-transit err = %v", err)
	}
	got, err := svc.Cancel(context.Background(), fx.requester, d.ID)
	if err != nil || got.Status != StatusCancelled {
		t.Fatalf("requester cancel = %+v err=%v", got, err)
	}
	replay, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{})
	if err != nil || replay.ID != d.ID || replay.Status != StatusCancelled {
		t.Fatalf("repeat create after cancel = %+v err=%v", replay, err)
	}
}

func TestConcurrentCreateReturnsSame(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	var wg sync.WaitGroup
	ids := make(chan ID, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{})
			if err != nil {
				errs <- err
				return
			}
			ids <- d.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("create err = %v", err)
	}
	var first ID
	for id := range ids {
		if first.IsZero() {
			first = id
			continue
		}
		if id != first {
			t.Fatalf("multiple deliveries %s vs %s", first, id)
		}
	}
}

func TestConcurrentDeliverOneLogicalWin(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkReady(context.Background(), fx.provider, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkInTransit(context.Background(), fx.provider, d.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.MarkDelivered(context.Background(), fx.provider, d.ID)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var ok int
	for err := range errs {
		if err == nil {
			ok++
			continue
		}
		t.Fatalf("deliver err = %v", err)
	}
	if ok != 2 {
		t.Fatalf("ok = %d want both idempotent or first+replay", ok)
	}
	got, err := svc.Get(context.Background(), fx.requester, d.ID)
	if err != nil || got.Status != StatusDelivered {
		t.Fatalf("got = %+v err=%v", got, err)
	}
}

type deliveryFixture struct {
	txnID     ID
	requester ID
	provider  ID
	status    string
	txns      *stubTxns
}

type stubTxns struct {
	fx    *deliveryFixture
	calls atomic.Int32
}

func (s *stubTxns) GetTransaction(ctx context.Context, transactionID txncontracts.ID) (txncontracts.TransactionRef, error) {
	s.calls.Add(1)
	if txncontracts.ID(s.fx.txnID) != transactionID {
		return txncontracts.TransactionRef{}, txncontracts.ErrNotFound
	}
	return txncontracts.TransactionRef{
		ID:              transactionID,
		RequesterUserID: txncontracts.ID(s.fx.requester),
		ProviderUserID:  txncontracts.ID(s.fx.provider),
		Status:          s.fx.status,
	}, nil
}

func TestGetDeliveryByTransactionContract(t *testing.T) {
	svc, fx := mustDeliveryService(t)
	if _, err := svc.GetDeliveryByTransaction(context.Background(), contracts.ID(fx.txnID)); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("missing delivery err = %v", err)
	}
	created, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := svc.GetDeliveryByTransaction(context.Background(), contracts.ID(fx.txnID))
	if err != nil || ref.ID != contracts.ID(created.ID) || ref.TransactionID != contracts.ID(fx.txnID) {
		t.Fatalf("ref = %+v err=%v", ref, err)
	}
}

func mustDeliveryService(t *testing.T) (*Service, *deliveryFixture) {
	t.Helper()
	fx := &deliveryFixture{
		txnID:     mustID(t),
		requester: mustID(t),
		provider:  mustID(t),
		status:    "pending",
	}
	txns := &stubTxns{fx: fx}
	fx.txns = txns
	clock := &frozenNow{now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(NewMemoryStore(), txns, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, fx
}

func methodPtr(m Method) *Method { return &m }

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time { return f.now }
