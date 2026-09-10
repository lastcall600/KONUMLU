package transactions

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	needcontracts "backend/internal/needs/contracts"
	offercontracts "backend/internal/offers/contracts"
	txncontracts "backend/internal/transactions/contracts"
)

func TestCreateFromAcceptedOfferIdempotentAndPrice(t *testing.T) {
	svc, fx := mustTxnService(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if txn.Status != StatusPending || txn.Price == nil || txn.Price.Amount != "250.50" {
		t.Fatalf("txn = %+v", txn)
	}
	again, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil || again.ID != txn.ID {
		t.Fatalf("idempotent = %+v err=%v", again, err)
	}
	if fx.needStatus != "open" {
		t.Fatalf("need mutated = %s", fx.needStatus)
	}
}

func TestCreateRejectsNonAcceptedAndUnrelated(t *testing.T) {
	svc, fx := mustTxnService(t)
	fx.offerStatus = "submitted"
	if _, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID); !errors.Is(err, errNotAccepted) {
		t.Fatalf("submitted err = %v", err)
	}
	fx.offerStatus = "accepted"
	if _, err := svc.CreateFromAcceptedOffer(context.Background(), mustID(t), fx.offerID); !errors.Is(err, errNotFound) {
		t.Fatalf("unrelated err = %v", err)
	}
	if _, err := svc.CreateFromAcceptedOffer(context.Background(), fx.provider, fx.offerID); !errors.Is(err, errNotFound) {
		t.Fatalf("provider create err = %v", err)
	}
	fx.needIDMismatch = true
	if _, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID); !errors.Is(err, errNotFound) {
		t.Fatalf("source mismatch err = %v", err)
	}
}

func TestCreateRejectedWhenNeedNotOpen(t *testing.T) {
	svc, fx := mustTxnService(t)
	cases := []struct {
		status string
		want   error
	}{
		{"cancelled", errNeedCancelled},
		{"expired", errNeedExpired},
		{"draft", errNeedDraft},
		{"fulfilled", errNeedFulfilled},
	}
	for _, tc := range cases {
		fx.needStatus = tc.status
		if _, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID); !errors.Is(err, tc.want) {
			t.Fatalf("status %s err = %v", tc.status, err)
		}
	}
	fx.needStatus = "open"
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil || txn.Status != StatusPending {
		t.Fatalf("open create = %+v err=%v", txn, err)
	}
}

func TestStartRejectedWhenNeedCancelledOrExpired(t *testing.T) {
	svc, fx := mustTxnService(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	fx.needStatus = "cancelled"
	if _, err := svc.Start(context.Background(), fx.requester, txn.ID); !errors.Is(err, errNeedCancelled) {
		t.Fatalf("cancelled start err = %v", err)
	}
	got, err := svc.Get(context.Background(), fx.requester, txn.ID)
	if err != nil || got.Status != StatusPending {
		t.Fatalf("txn mutated = %+v err=%v", got, err)
	}
	if fx.needStatus != "cancelled" {
		t.Fatalf("need mutated = %s", fx.needStatus)
	}

	svc2, fx2 := mustTxnService(t)
	txn2, err := svc2.CreateFromAcceptedOffer(context.Background(), fx2.requester, fx2.offerID)
	if err != nil {
		t.Fatal(err)
	}
	fx2.needStatus = "expired"
	if _, err := svc2.Start(context.Background(), fx2.requester, txn2.ID); !errors.Is(err, errNeedExpired) {
		t.Fatalf("expired start err = %v", err)
	}
	fx2.needStatus = "draft"
	if _, err := svc2.Start(context.Background(), fx2.requester, txn2.ID); !errors.Is(err, errNeedDraft) {
		t.Fatalf("draft start err = %v", err)
	}
	fx2.needStatus = "fulfilled"
	if _, err := svc2.Start(context.Background(), fx2.requester, txn2.ID); !errors.Is(err, errNeedFulfilled) {
		t.Fatalf("fulfilled start err = %v", err)
	}
}

func TestCompleteRejectedWhenNeedCancelledOrExpired(t *testing.T) {
	svc, fx := mustTxnService(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), fx.requester, txn.ID); err != nil {
		t.Fatal(err)
	}
	fx.needStatus = "cancelled"
	if _, err := svc.Complete(context.Background(), fx.requester, txn.ID); !errors.Is(err, errNeedCancelled) {
		t.Fatalf("cancelled complete err = %v", err)
	}
	got, err := svc.Get(context.Background(), fx.requester, txn.ID)
	if err != nil || got.Status != StatusActive {
		t.Fatalf("txn mutated = %+v err=%v", got, err)
	}

	svc2, fx2 := mustTxnService(t)
	txn2, err := svc2.CreateFromAcceptedOffer(context.Background(), fx2.requester, fx2.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.Start(context.Background(), fx2.requester, txn2.ID); err != nil {
		t.Fatal(err)
	}
	fx2.needStatus = "expired"
	if _, err := svc2.Complete(context.Background(), fx2.requester, txn2.ID); !errors.Is(err, errNeedExpired) {
		t.Fatalf("expired complete err = %v", err)
	}
	fx2.needStatus = "draft"
	if _, err := svc2.Complete(context.Background(), fx2.requester, txn2.ID); !errors.Is(err, errNeedDraft) {
		t.Fatalf("draft complete err = %v", err)
	}
}

func TestCompleteAllowsOpenAndAlreadyFulfilledNeed(t *testing.T) {
	svc, fx := mustTxnService(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), fx.requester, txn.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.Complete(context.Background(), fx.requester, txn.ID)
	if err != nil || completed.Status != StatusCompleted {
		t.Fatalf("open complete = %+v err=%v", completed, err)
	}
	if fx.needStatus != "open" {
		t.Fatalf("complete mutated need = %s", fx.needStatus)
	}

	svc2, fx2 := mustTxnService(t)
	txn2, err := svc2.CreateFromAcceptedOffer(context.Background(), fx2.requester, fx2.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.Start(context.Background(), fx2.requester, txn2.ID); err != nil {
		t.Fatal(err)
	}
	fx2.needStatus = "fulfilled"
	again, err := svc2.Complete(context.Background(), fx2.requester, txn2.ID)
	if err != nil || again.Status != StatusCompleted {
		t.Fatalf("already fulfilled complete = %+v err=%v", again, err)
	}
	replay, err := svc2.Complete(context.Background(), fx2.requester, txn2.ID)
	if err != nil || replay.ID != again.ID {
		t.Fatalf("idempotent complete = %+v err=%v", replay, err)
	}
}

func TestParticipantAccessAndPrivacyLifecycle(t *testing.T) {
	svc, fx := mustTxnService(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), mustID(t), txn.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger get err = %v", err)
	}
	got, err := svc.Get(context.Background(), fx.provider, txn.ID)
	if err != nil || got.ID != txn.ID {
		t.Fatalf("provider get = %+v err=%v", got, err)
	}
	listed, err := svc.ListMine(context.Background(), fx.requester)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %+v err=%v", listed, err)
	}
	active, err := svc.Start(context.Background(), fx.provider, txn.ID)
	if err != nil || active.Status != StatusActive {
		t.Fatalf("start = %+v err=%v", active, err)
	}
	if _, err := svc.Complete(context.Background(), fx.requester, txn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), fx.requester, txn.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
	if fx.needStatus != "open" {
		t.Fatalf("need fulfilled = %s", fx.needStatus)
	}
}

func TestInvalidJumpsAndCancel(t *testing.T) {
	svc, fx := mustTxnService(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Complete(context.Background(), fx.requester, txn.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("pending complete err = %v", err)
	}
	cancelled, err := svc.Cancel(context.Background(), fx.requester, txn.ID)
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("cancel = %+v err=%v", cancelled, err)
	}
	same, err := svc.Cancel(context.Background(), fx.provider, txn.ID)
	if err != nil || same.ID != cancelled.ID {
		t.Fatalf("idempotent cancel = %+v err=%v", same, err)
	}
	if _, err := svc.Start(context.Background(), fx.requester, txn.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("cancelled start err = %v", err)
	}
}

func TestGetTransactionContractHidesNothingNeededForPayment(t *testing.T) {
	svc, fx := mustTxnService(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := svc.GetTransaction(context.Background(), txncontracts.ID(txn.ID))
	if err != nil {
		t.Fatal(err)
	}
	if txncontracts.ID(txn.ID) != ref.ID || txncontracts.ID(fx.requester) != ref.RequesterUserID ||
		txncontracts.ID(fx.provider) != ref.ProviderUserID || ref.Price == nil || ref.Price.Amount != "250.50" {
		t.Fatalf("ref = %+v", ref)
	}
	if _, err := svc.GetTransaction(context.Background(), txncontracts.ID(mustID(t))); !errors.Is(err, txncontracts.ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestConcurrentCreateReturnsSame(t *testing.T) {
	svc, fx := mustTxnService(t)
	var wg sync.WaitGroup
	ids := make(chan ID, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
			if err != nil {
				errs <- err
				return
			}
			ids <- txn.ID
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
			t.Fatalf("multiple transactions %s vs %s", first, id)
		}
	}
}

func TestConcurrentCompleteOneLogicalWin(t *testing.T) {
	svc, fx := mustTxnService(t)
	txn, err := svc.CreateFromAcceptedOffer(context.Background(), fx.requester, fx.offerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), fx.requester, txn.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Complete(context.Background(), fx.requester, txn.ID)
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
		t.Fatalf("complete err = %v", err)
	}
	if ok != 2 {
		t.Fatalf("ok = %d want both idempotent or first+replay", ok)
	}
	got, err := svc.Get(context.Background(), fx.requester, txn.ID)
	if err != nil || got.Status != StatusCompleted {
		t.Fatalf("got = %+v err=%v", got, err)
	}
}

type txnFixture struct {
	offerID        ID
	needID         ID
	requester      ID
	provider       ID
	businessID     ID
	serviceID      ID
	offerStatus    string
	needStatus     string
	needIDMismatch bool
}

type stubOffers struct {
	fx *txnFixture
}

func (s stubOffers) GetOffer(ctx context.Context, offerID offercontracts.ID) (offercontracts.OfferRef, error) {
	if offercontracts.ID(s.fx.offerID) != offerID {
		return offercontracts.OfferRef{}, offercontracts.ErrNotFound
	}
	return offercontracts.OfferRef{
		ID:                 offerID,
		NeedID:             offercontracts.ID(s.fx.needID),
		ProviderBusinessID: offercontracts.ID(s.fx.businessID),
		ServiceID:          offercontracts.ID(s.fx.serviceID),
		ProviderUserID:     offercontracts.ID(s.fx.provider),
		Price:              &offercontracts.Price{Amount: "250.50", Currency: "TRY"},
		Status:             s.fx.offerStatus,
	}, nil
}

type stubNeeds struct {
	fx *txnFixture
}

func (s stubNeeds) GetNeed(ctx context.Context, needID needcontracts.ID) (needcontracts.NeedRef, error) {
	if needcontracts.ID(s.fx.needID) != needID {
		return needcontracts.NeedRef{}, needcontracts.ErrNotFound
	}
	id := needID
	if s.fx.needIDMismatch {
		id = needcontracts.ID(mustIDFromFixture(s.fx.offerID))
	}
	return needcontracts.NeedRef{
		ID:              id,
		RequesterUserID: needcontracts.ID(s.fx.requester),
		Status:          s.fx.needStatus,
		Title:           "Need",
	}, nil
}

func mustIDFromFixture(seed ID) ID {
	var id ID
	copy(id[:], seed[:])
	id[15] ^= 0xff
	if id.IsZero() {
		id[0] = 1
	}
	return id
}

func (s stubNeeds) AssertOwnedBy(ctx context.Context, needID, userID needcontracts.ID) error {
	ref, err := s.GetNeed(ctx, needID)
	if err != nil {
		return err
	}
	if ref.RequesterUserID != userID {
		return needcontracts.ErrNotFound
	}
	return nil
}

func mustTxnService(t *testing.T) (*Service, *txnFixture) {
	t.Helper()
	fx := &txnFixture{
		offerID:     mustID(t),
		needID:      mustID(t),
		requester:   mustID(t),
		provider:    mustID(t),
		businessID:  mustID(t),
		serviceID:   mustID(t),
		offerStatus: "accepted",
		needStatus:  "open",
	}
	clock := &frozenNow{now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	svc, err := NewService(NewMemoryStore(), stubOffers{fx: fx}, stubNeeds{fx: fx}, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, fx
}

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time { return f.now }
