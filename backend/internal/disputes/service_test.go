package disputes

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	delcontracts "backend/internal/deliveries/contracts"
	txncontracts "backend/internal/transactions/contracts"
)

func TestParticipantOpensDisputeAndStrangerDenied(t *testing.T) {
	svc, fx := mustDisputeService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonOther})
	if err != nil {
		t.Fatal(err)
	}
	if d.OpenedByUserID != fx.requester || d.Status != StatusOpen {
		t.Fatalf("d = %+v", d)
	}
	if _, err := svc.CreateForTransaction(context.Background(), mustID(t), fx.txnID, CreateInput{ReasonCode: ReasonOther}); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger create err = %v", err)
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

func TestOpenedByComesFromSessionNotClient(t *testing.T) {
	svc, fx := mustDisputeService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.provider, fx.txnID, CreateInput{ReasonCode: ReasonPaymentIssue})
	if err != nil {
		t.Fatal(err)
	}
	if d.OpenedByUserID != fx.provider || d.OpenedByRole() != RoleProvider {
		t.Fatalf("openedBy = %s role=%s", d.OpenedByUserID, d.OpenedByRole())
	}
}

func TestEligibleAndIneligibleTransactionStates(t *testing.T) {
	svc, fx := mustDisputeService(t)
	fx.status = "cancelled"
	if _, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonCancellationIssue}); !errors.Is(err, errNotEligible) {
		t.Fatalf("cancelled err = %v", err)
	}
	fx.status = "completed"
	completed := fx.created.Add(24 * time.Hour)
	fx.completedAt = &completed
	if _, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonDamagedOrIncomplete}); err != nil {
		t.Fatalf("completed err = %v", err)
	}
}

func TestWindowPolicy(t *testing.T) {
	svc, fx := mustDisputeService(t)
	fx.now.now = fx.created.Add(DefaultWindow + time.Second)
	if _, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonOther}); !errors.Is(err, errWindowClosed) {
		t.Fatalf("window err = %v", err)
	}
}

func TestDuplicateActiveDisputeIsIdempotent(t *testing.T) {
	svc, fx := mustDisputeService(t)
	first, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonNonDelivery})
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.CreateForTransaction(context.Background(), fx.provider, fx.txnID, CreateInput{ReasonCode: ReasonPaymentIssue})
	if err != nil || again.ID != first.ID || again.ReasonCode != first.ReasonCode {
		t.Fatalf("idempotent = %+v err=%v", again, err)
	}
}

func TestConcurrentCreateReturnsSame(t *testing.T) {
	svc, fx := mustDisputeService(t)
	var wg sync.WaitGroup
	ids := make(chan ID, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonOther})
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
			t.Fatalf("multiple disputes %s vs %s", first, id)
		}
	}
}

func TestInvalidReasonRejected(t *testing.T) {
	svc, fx := mustDisputeService(t)
	if _, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: "refund_now"}); !errors.Is(err, errInvalidReason) {
		t.Fatalf("reason err = %v", err)
	}
}

func TestLifecycleValidInvalidAndNoReopen(t *testing.T) {
	svc, fx := mustDisputeService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonOther})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(context.Background(), d.ID, ResolutionNoAction); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("skip review err = %v", err)
	}
	review, err := svc.StartReview(context.Background(), d.ID)
	if err != nil || review.Status != StatusUnderReview {
		t.Fatalf("review = %+v err=%v", review, err)
	}
	if _, err := svc.Withdraw(context.Background(), fx.requester, d.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("withdraw under review err = %v", err)
	}
	resolved, err := svc.Resolve(context.Background(), d.ID, ResolutionInsufficientEvidence)
	if err != nil || resolved.Status != StatusResolved {
		t.Fatalf("resolved = %+v err=%v", resolved, err)
	}
	closed, err := svc.Close(context.Background(), d.ID)
	if err != nil || closed.Status != StatusClosed {
		t.Fatalf("closed = %+v err=%v", closed, err)
	}
	if _, err := svc.StartReview(context.Background(), d.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
	if _, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonOther}); !errors.Is(err, errConcluded) {
		t.Fatalf("second after close err = %v", err)
	}
}

func TestWithdrawThenNewDisputeAllowed(t *testing.T) {
	svc, fx := mustDisputeService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonOther})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Withdraw(context.Background(), fx.requester, d.ID); err != nil {
		t.Fatal(err)
	}
	next, err := svc.CreateForTransaction(context.Background(), fx.provider, fx.txnID, CreateInput{ReasonCode: ReasonOther})
	if err != nil || next.ID == d.ID {
		t.Fatalf("new after withdraw = %+v err=%v", next, err)
	}
}

func TestEvidenceAppendOnlyAndOwnership(t *testing.T) {
	svc, fx := mustDisputeService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonOther})
	if err != nil {
		t.Fatal(err)
	}
	e, err := svc.AddEvidence(context.Background(), fx.requester, d.ID, EvidenceInput{
		EvidenceType: EvidencePartyStatement,
		Title:        "what happened",
	})
	if err != nil || e.ActorRole != RoleRequester {
		t.Fatalf("evidence = %+v err=%v", e, err)
	}
	if _, err := svc.AddEvidence(context.Background(), mustID(t), d.ID, EvidenceInput{
		EvidenceType: EvidencePartyStatement,
		Title:        "intruder",
	}); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger evidence err = %v", err)
	}
	if _, err := svc.AddEvidence(context.Background(), fx.requester, d.ID, EvidenceInput{
		EvidenceType: EvidenceInternalReference,
		Title:        "secret",
	}); !errors.Is(err, errInvalidEvidence) {
		t.Fatalf("consumer internal err = %v", err)
	}
	listed, err := svc.ListEvidence(context.Background(), fx.provider, d.ID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list evidence = %+v err=%v", listed, err)
	}
	if _, err := svc.Withdraw(context.Background(), fx.requester, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddEvidence(context.Background(), fx.requester, d.ID, EvidenceInput{
		EvidenceType: EvidencePartyStatement,
		Title:        "late",
	}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("terminal evidence err = %v", err)
	}
}

func TestResolutionDoesNotMutatePeers(t *testing.T) {
	svc, fx := mustDisputeService(t)
	d, err := svc.CreateForTransaction(context.Background(), fx.requester, fx.txnID, CreateInput{ReasonCode: ReasonNonDelivery})
	if err != nil {
		t.Fatal(err)
	}
	if fx.txns.gets.Load() < 1 {
		t.Fatal("expected Transactions Lookup")
	}
	if fx.dels.gets.Load() < 1 {
		t.Fatal("expected optional Deliveries read for non_delivery")
	}
	if _, err := svc.StartReview(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(context.Background(), d.ID, ResolutionNoAction); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Close(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if fx.txns.writes.Load() != 0 || fx.dels.writes.Load() != 0 || fx.payments.Load() != 0 || fx.trust.Load() != 0 {
		t.Fatalf("peer writes txn=%d del=%d pay=%d trust=%d", fx.txns.writes.Load(), fx.dels.writes.Load(), fx.payments.Load(), fx.trust.Load())
	}
	if fx.status != "pending" {
		t.Fatalf("transaction status mutated: %s", fx.status)
	}
}

type disputeFixture struct {
	txnID       ID
	requester   ID
	provider    ID
	status      string
	created     time.Time
	completedAt *time.Time
	now         *frozenNow
	txns        *stubTxns
	dels        *stubDeliveries
	payments    atomic.Int32
	trust       atomic.Int32
}

type stubTxns struct {
	fx     *disputeFixture
	gets   atomic.Int32
	writes atomic.Int32
}

func (s *stubTxns) GetTransaction(ctx context.Context, transactionID txncontracts.ID) (txncontracts.TransactionRef, error) {
	s.gets.Add(1)
	if txncontracts.ID(s.fx.txnID) != transactionID {
		return txncontracts.TransactionRef{}, txncontracts.ErrNotFound
	}
	return txncontracts.TransactionRef{
		ID:              transactionID,
		RequesterUserID: txncontracts.ID(s.fx.requester),
		ProviderUserID:  txncontracts.ID(s.fx.provider),
		Status:          s.fx.status,
		CreatedAt:       s.fx.created,
		CompletedAt:     s.fx.completedAt,
	}, nil
}

type stubDeliveries struct {
	gets   atomic.Int32
	writes atomic.Int32
}

func (s *stubDeliveries) GetDelivery(ctx context.Context, deliveryID delcontracts.ID) (delcontracts.DeliveryRef, error) {
	s.gets.Add(1)
	return delcontracts.DeliveryRef{}, delcontracts.ErrNotFound
}

func (s *stubDeliveries) GetDeliveryByTransaction(ctx context.Context, transactionID delcontracts.ID) (delcontracts.DeliveryRef, error) {
	s.gets.Add(1)
	return delcontracts.DeliveryRef{}, delcontracts.ErrNotFound
}

func mustDisputeService(t *testing.T) (*Service, *disputeFixture) {
	t.Helper()
	created := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	fx := &disputeFixture{
		txnID:     mustID(t),
		requester: mustID(t),
		provider:  mustID(t),
		status:    "pending",
		created:   created,
		now:       &frozenNow{now: created.Add(24 * time.Hour)},
	}
	txns := &stubTxns{fx: fx}
	dels := &stubDeliveries{}
	fx.txns = txns
	fx.dels = dels
	svc, err := NewService(NewMemoryStore(), txns, dels, DefaultPolicy(), fx.now.Now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, fx
}

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time { return f.now }
