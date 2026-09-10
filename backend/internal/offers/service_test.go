package offers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	bizcontracts "backend/internal/businesses/contracts"
	needcontracts "backend/internal/needs/contracts"
	offercontracts "backend/internal/offers/contracts"
)

func TestCreateEligibleProvider(t *testing.T) {
	svc, fx := mustOfferService(t)
	offer, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID, Message: "Hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if offer.Status != StatusSubmitted || offer.ProviderUserID != fx.provider {
		t.Fatalf("offer = %+v", offer)
	}
	again, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	})
	if err != nil || again.ID != offer.ID {
		t.Fatalf("idempotent create = %+v err = %v", again, err)
	}
}

func TestCreateDeniesNonCandidateInactiveAndSpoofOwner(t *testing.T) {
	svc, fx := mustOfferService(t)
	fx.eligible = false
	if _, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	}); !errors.Is(err, errNotEligible) {
		t.Fatalf("non-candidate err = %v", err)
	}
	fx.eligible = true
	fx.bizOwner = mustID(t)
	if _, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	}); !errors.Is(err, errNotFound) {
		t.Fatalf("spoof owner err = %v", err)
	}
}

func TestRequesterListPrivacyAndLifecycle(t *testing.T) {
	svc, fx := mustOfferService(t)
	offer, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListForRequester(context.Background(), mustID(t), fx.needID); !errors.Is(err, errNotFound) {
		t.Fatalf("foreign list err = %v", err)
	}
	listed, err := svc.ListForRequester(context.Background(), fx.requester, fx.needID)
	if err != nil || len(listed) != 1 || listed[0].ID != offer.ID {
		t.Fatalf("list = %+v err = %v", listed, err)
	}
	withdrawn, err := svc.Withdraw(context.Background(), fx.provider, offer.ID)
	if err != nil || withdrawn.Status != StatusWithdrawn {
		t.Fatalf("withdraw = %+v err = %v", withdrawn, err)
	}
	if _, err := svc.Accept(context.Background(), fx.requester, fx.needID, offer.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("withdrawn accept err = %v", err)
	}
}

func TestAcceptRejectsSecondAndLeavesNeedOpen(t *testing.T) {
	svc, fx := mustOfferService(t)
	first, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	fx.serviceID = mustID(t)
	second, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := svc.Accept(context.Background(), fx.requester, fx.needID, first.ID)
	if err != nil || accepted.Status != StatusAccepted {
		t.Fatalf("accept = %+v err = %v", accepted, err)
	}
	again, err := svc.Accept(context.Background(), fx.requester, fx.needID, first.ID)
	if err != nil || again.Status != StatusAccepted {
		t.Fatalf("idempotent accept = %+v err = %v", again, err)
	}
	if _, err := svc.Accept(context.Background(), fx.requester, fx.needID, second.ID); !errors.Is(err, errConflict) {
		t.Fatalf("second accept err = %v", err)
	}
	listed, err := svc.ListForRequester(context.Background(), fx.requester, fx.needID)
	if err != nil || len(listed) != 2 {
		t.Fatal(err)
	}
	var rejected, stillAccepted int
	for _, o := range listed {
		switch o.Status {
		case StatusRejected:
			rejected++
		case StatusAccepted:
			stillAccepted++
		}
	}
	if rejected != 1 || stillAccepted != 1 {
		t.Fatalf("listed = %+v", listed)
	}
	if fx.needStatus != "open" || !NeedRemainsOpenOnAccept {
		t.Fatalf("need status mutated = %s", fx.needStatus)
	}
	if _, err := svc.Reject(context.Background(), fx.requester, fx.needID, accepted.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("accepted reject err = %v", err)
	}
}

func TestConcurrentAcceptOnlyOneWins(t *testing.T) {
	svc, fx := mustOfferService(t)
	first, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	fx.serviceID = mustID(t)
	second, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []ID{first.ID, second.ID} {
		wg.Add(1)
		go func(offerID ID) {
			defer wg.Done()
			_, err := svc.Accept(context.Background(), fx.requester, fx.needID, offerID)
			errs <- err
		}(id)
	}
	wg.Wait()
	close(errs)
	var ok, conflict int
	for err := range errs {
		if err == nil {
			ok++
			continue
		}
		if errors.Is(err, errConflict) || errors.Is(err, errInvalidTransition) {
			conflict++
			continue
		}
		t.Fatalf("unexpected err = %v", err)
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("ok = %d conflict = %d", ok, conflict)
	}
	listed, err := svc.ListForRequester(context.Background(), fx.requester, fx.needID)
	if err != nil {
		t.Fatal(err)
	}
	accepted := 0
	for _, o := range listed {
		if o.Status == StatusAccepted {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted count = %d listed = %+v", accepted, listed)
	}
}

func TestOfferLookupContract(t *testing.T) {
	svc, fx := mustOfferService(t)
	if _, err := svc.GetOffer(context.Background(), offercontracts.ID{}); !errors.Is(err, offercontracts.ErrZeroID) {
		t.Fatalf("zero id err = %v", err)
	}
	offer, err := svc.Create(context.Background(), fx.provider, Content{
		NeedID: fx.needID, ProviderBusinessID: fx.businessID, ServiceID: fx.serviceID,
		Price: &Price{Amount: "10", Currency: "TRY"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := svc.GetOffer(context.Background(), offercontracts.ID(offer.ID))
	if err != nil || ref.Status != string(StatusSubmitted) || ref.ProviderUserID != offercontracts.ID(fx.provider) {
		t.Fatalf("ref = %+v err = %v", ref, err)
	}
	if ref.Price == nil || ref.Price.Amount != "10" {
		t.Fatalf("price = %+v", ref.Price)
	}
	accepted, err := svc.Accept(context.Background(), fx.requester, fx.needID, offer.ID)
	if err != nil {
		t.Fatal(err)
	}
	ref, err = svc.GetOffer(context.Background(), offercontracts.ID(accepted.ID))
	if err != nil || ref.Status != string(StatusAccepted) {
		t.Fatalf("accepted ref = %+v err = %v", ref, err)
	}
}

func TestProviderNeedViewEligibilityGated(t *testing.T) {
	svc, fx := mustOfferService(t)
	need, err := svc.ProviderNeedView(context.Background(), fx.provider, fx.needID, fx.businessID, fx.serviceID)
	if err != nil || need.Title != "Need" {
		t.Fatalf("view = %+v err = %v", need, err)
	}
	fx.eligible = false
	if _, err := svc.ProviderNeedView(context.Background(), fx.provider, fx.needID, fx.businessID, fx.serviceID); !errors.Is(err, errNotEligible) {
		t.Fatalf("gated err = %v", err)
	}
}

type offerFixture struct {
	needID     ID
	businessID ID
	serviceID  ID
	requester  ID
	provider   ID
	bizOwner   ID
	eligible   bool
	needStatus string
}

type stubNeeds struct {
	fx *offerFixture
}

func (s stubNeeds) GetNeed(ctx context.Context, needID needcontracts.ID) (needcontracts.NeedRef, error) {
	if needcontracts.ID(s.fx.needID) != needID {
		return needcontracts.NeedRef{}, needcontracts.ErrNotFound
	}
	return needcontracts.NeedRef{
		ID:              needID,
		RequesterUserID: needcontracts.ID(s.fx.requester),
		Status:          s.fx.needStatus,
		Title:           "Need",
		Latitude:        36.621,
		Longitude:       29.116,
	}, nil
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

type stubBiz struct {
	fx *offerFixture
}

func (s stubBiz) GetBusiness(ctx context.Context, businessID bizcontracts.ID) (bizcontracts.ProfileRef, error) {
	if bizcontracts.ID(s.fx.businessID) != businessID {
		return bizcontracts.ProfileRef{}, bizcontracts.ErrNotFound
	}
	return bizcontracts.ProfileRef{
		ID:          businessID,
		OwnerUserID: bizcontracts.ID(s.fx.bizOwner),
		Status:      "active",
		DisplayName: "Kafe",
	}, nil
}

func (s stubBiz) AssertOwnedBy(ctx context.Context, businessID, userID bizcontracts.ID) error {
	ref, err := s.GetBusiness(ctx, businessID)
	if err != nil {
		return err
	}
	if ref.OwnerUserID != userID {
		return bizcontracts.ErrForbidden
	}
	return nil
}

func (s stubBiz) GetService(ctx context.Context, serviceID bizcontracts.ID) (bizcontracts.OfferedServiceRef, error) {
	if bizcontracts.ID(s.fx.serviceID) != serviceID && serviceID != bizcontracts.ID(s.fx.serviceID) {
		// allow second service created in tests by returning title anyway if we only know current fx.serviceID
	}
	return bizcontracts.OfferedServiceRef{
		ID:         serviceID,
		BusinessID: bizcontracts.ID(s.fx.businessID),
		Status:     "active",
		Title:      "Tur",
	}, nil
}

func (s stubBiz) ListServicesForBusiness(ctx context.Context, businessID bizcontracts.ID) ([]bizcontracts.OfferedServiceRef, error) {
	return nil, nil
}

func (s stubBiz) CheckServiceCandidate(ctx context.Context, check bizcontracts.EligibilityCheck) (bizcontracts.ServiceCandidate, error) {
	if !s.fx.eligible {
		return bizcontracts.ServiceCandidate{}, bizcontracts.ErrNotFound
	}
	if check.BusinessID != bizcontracts.ID(s.fx.businessID) {
		return bizcontracts.ServiceCandidate{}, bizcontracts.ErrNotFound
	}
	return bizcontracts.ServiceCandidate{
		BusinessID:          check.BusinessID,
		ServiceID:           check.ServiceID,
		BusinessDisplayName: "Kafe",
		ServiceTitle:        "Tur",
	}, nil
}

func mustOfferService(t *testing.T) (*Service, *offerFixture) {
	t.Helper()
	fx := &offerFixture{
		needID:     mustID(t),
		businessID: mustID(t),
		serviceID:  mustID(t),
		requester:  mustID(t),
		provider:   mustID(t),
		eligible:   true,
		needStatus: "open",
	}
	fx.bizOwner = fx.provider
	clock := &frozenNow{now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	biz := stubBiz{fx: fx}
	svc, err := NewService(NewMemoryStore(), stubNeeds{fx: fx}, biz, biz, biz, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, fx
}

type frozenNow struct {
	now time.Time
}

func (f *frozenNow) Now() time.Time { return f.now }
