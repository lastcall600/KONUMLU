package payments

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	txncontracts "backend/internal/transactions/contracts"
)

func TestCreateFromPricedTransactionCopiesAmountAndPayer(t *testing.T) {
	svc, fx := mustPayService(t)
	p, err := svc.CreateForTransaction(context.Background(), fx.payer, fx.txnID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusPending || p.Amount.Amount != "250.50" || p.Amount.Currency != "TRY" {
		t.Fatalf("p = %+v", p)
	}
	if p.PayerUserID != fx.payer || p.PayeeUserID != fx.payee {
		t.Fatalf("parties = payer %s payee %s", p.PayerUserID, p.PayeeUserID)
	}
	again, err := svc.CreateForTransaction(context.Background(), fx.payer, fx.txnID)
	if err != nil || again.ID != p.ID {
		t.Fatalf("idempotent = %+v err=%v", again, err)
	}
}

func TestUnpricedAndNotPayableRejected(t *testing.T) {
	svc, fx := mustPayService(t)
	fx.price = nil
	if _, err := svc.CreateForTransaction(context.Background(), fx.payer, fx.txnID); !errors.Is(err, errNotPriced) {
		t.Fatalf("unpriced err = %v", err)
	}
	fx.price = &txncontracts.Price{Amount: "10", Currency: "TRY"}
	fx.status = "cancelled"
	if _, err := svc.CreateForTransaction(context.Background(), fx.payer, fx.txnID); !errors.Is(err, errNotPayable) {
		t.Fatalf("cancelled err = %v", err)
	}
	fx.status = "completed"
	if _, err := svc.CreateForTransaction(context.Background(), fx.payer, fx.txnID); !errors.Is(err, errNotPayable) {
		t.Fatalf("completed err = %v", err)
	}
}

func TestCreateDerivesPayerAndDeniesUnrelated(t *testing.T) {
	svc, fx := mustPayService(t)
	if _, err := svc.CreateForTransaction(context.Background(), fx.payee, fx.txnID); !errors.Is(err, errNotFound) {
		t.Fatalf("payee create err = %v", err)
	}
	if _, err := svc.CreateForTransaction(context.Background(), mustID(t), fx.txnID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger create err = %v", err)
	}
}

func TestParticipantReadAndUnrelatedNotFound(t *testing.T) {
	svc, fx := mustPayService(t)
	p, err := svc.CreateForTransaction(context.Background(), fx.payer, fx.txnID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), mustID(t), p.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger get err = %v", err)
	}
	got, err := svc.Get(context.Background(), fx.payee, p.ID)
	if err != nil || got.ID != p.ID {
		t.Fatalf("payee get = %+v err=%v", got, err)
	}
	listed, err := svc.ListMine(context.Background(), fx.payer)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %+v err=%v", listed, err)
	}
}

func TestProviderCallbackLifecycleIdempotent(t *testing.T) {
	svc, fx := mustPayService(t)
	p, err := svc.CreateForTransaction(context.Background(), fx.payer, fx.txnID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyCaptured(context.Background(), p.ID, ProviderSignal{}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("pending capture err = %v", err)
	}
	ref := "psp-ref-1"
	prov := "placeholder"
	auth, err := svc.ApplyAuthorized(context.Background(), p.ID, ProviderSignal{Provider: &prov, ProviderReference: &ref})
	if err != nil || auth.Status != StatusAuthorized || auth.ProviderReference == nil || *auth.ProviderReference != ref {
		t.Fatalf("authorize = %+v err=%v", auth, err)
	}
	same, err := svc.ApplyAuthorized(context.Background(), p.ID, ProviderSignal{ProviderReference: &ref})
	if err != nil || same.Status != StatusAuthorized {
		t.Fatalf("duplicate authorize = %+v err=%v", same, err)
	}
	captured, err := svc.ApplyCaptured(context.Background(), p.ID, ProviderSignal{})
	if err != nil || captured.Status != StatusCaptured {
		t.Fatalf("capture = %+v err=%v", captured, err)
	}
	replay, err := svc.ApplyCaptured(context.Background(), p.ID, ProviderSignal{})
	if err != nil || replay.Status != StatusCaptured {
		t.Fatalf("duplicate capture = %+v err=%v", replay, err)
	}
	if _, err := svc.ApplyCancelled(context.Background(), p.ID, ProviderSignal{}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("captured cancel err = %v", err)
	}
}

func TestConcurrentCreateReturnsSame(t *testing.T) {
	svc, fx := mustPayService(t)
	var wg sync.WaitGroup
	ids := make(chan ID, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := svc.CreateForTransaction(context.Background(), fx.payer, fx.txnID)
			if err != nil {
				errs <- err
				return
			}
			ids <- p.ID
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
			t.Fatalf("multiple payments %s vs %s", first, id)
		}
	}
}

func TestConcurrentCaptureOneLogicalWin(t *testing.T) {
	svc, fx := mustPayService(t)
	p, err := svc.CreateForTransaction(context.Background(), fx.payer, fx.txnID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyAuthorized(context.Background(), p.ID, ProviderSignal{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.ApplyCaptured(context.Background(), p.ID, ProviderSignal{})
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
		t.Fatalf("capture err = %v", err)
	}
	if ok != 2 {
		t.Fatalf("ok = %d want both idempotent or first+replay", ok)
	}
	got, err := svc.Get(context.Background(), fx.payer, p.ID)
	if err != nil || got.Status != StatusCaptured {
		t.Fatalf("got = %+v err=%v", got, err)
	}
}

type payFixture struct {
	txnID  ID
	payer  ID
	payee  ID
	status string
	price  *txncontracts.Price
}

type stubTxns struct {
	fx *payFixture
}

func (s stubTxns) GetTransaction(ctx context.Context, transactionID txncontracts.ID) (txncontracts.TransactionRef, error) {
	if txncontracts.ID(s.fx.txnID) != transactionID {
		return txncontracts.TransactionRef{}, txncontracts.ErrNotFound
	}
	return txncontracts.TransactionRef{
		ID:              transactionID,
		RequesterUserID: txncontracts.ID(s.fx.payer),
		ProviderUserID:  txncontracts.ID(s.fx.payee),
		Price:           s.fx.price,
		Status:          s.fx.status,
	}, nil
}

func mustPayService(t *testing.T) (*Service, *payFixture) {
	t.Helper()
	fx := &payFixture{
		txnID:  mustID(t),
		payer:  mustID(t),
		payee:  mustID(t),
		status: "pending",
		price:  &txncontracts.Price{Amount: "250.50", Currency: "TRY"},
	}
	clock := &frozenNow{now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(NewMemoryStore(), stubTxns{fx: fx}, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, fx
}

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time { return f.now }
