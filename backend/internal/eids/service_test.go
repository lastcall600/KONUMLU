package eids

import (
	"context"
	"errors"
	"testing"
	"time"

	listingcontracts "backend/internal/listings/contracts"
	mdcontracts "backend/internal/masterdata/contracts"
)

func TestStartIsIdempotentAndTypeAware(t *testing.T) {
	fx := newFixture(t)
	fx.policy.req = mdcontracts.EIDSRequirementProperty
	fx.gateway.property = ProviderOutcome{Outcome: OutcomeVerified}

	first, err := fx.svc.StartForOwner(context.Background(), fx.owner, fx.listing, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fx.svc.StartForOwner(context.Background(), fx.owner, fx.listing, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("duplicate active rows %s vs %s", first.ID, second.ID)
	}
	if fx.gateway.propertyCalls != 1 {
		t.Fatalf("verified start must not re-call provider, calls=%d", fx.gateway.propertyCalls)
	}
	if _, err := fx.svc.StartForOwner(context.Background(), fx.owner, fx.listing, "vehicle"); !errors.Is(err, errWrongType) {
		t.Fatalf("wrong type err = %v", err)
	}
	ok, err := fx.svc.IsVerified(context.Background(), fx.listing, TypeProperty)
	if err != nil || !ok {
		t.Fatalf("property verified = %v err=%v", ok, err)
	}
	ok, err = fx.svc.IsVerified(context.Background(), fx.listing, TypeVehicle)
	if err != nil || ok {
		t.Fatalf("vehicle must not pass from property: %v err=%v", ok, err)
	}
}

func TestUnavailableBlocksEligibilityAndAllowsRetry(t *testing.T) {
	fx := newFixture(t)
	fx.policy.req = mdcontracts.EIDSRequirementVehicle
	fx.gateway.vehicle = ProviderOutcome{Outcome: OutcomeUnavailable}
	got, err := fx.svc.StartForOwner(context.Background(), fx.owner, fx.listing, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusUnavailable {
		t.Fatalf("status = %s", got.Status)
	}
	ok, err := fx.svc.IsVerified(context.Background(), fx.listing, TypeVehicle)
	if err != nil || ok {
		t.Fatalf("unavailable must not be eligible")
	}
	fx.gateway.vehicle = ProviderOutcome{Outcome: OutcomeVerified}
	retried, err := fx.svc.StartForOwner(context.Background(), fx.owner, fx.listing, "")
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID != got.ID {
		t.Fatal("retry must reuse open unavailable row")
	}
	if retried.Status != StatusVerified {
		t.Fatalf("retry status = %s", retried.Status)
	}
}

func TestFailedAndExpiredBlockPublish(t *testing.T) {
	fx := newFixture(t)
	fx.policy.req = mdcontracts.EIDSRequirementProperty
	fx.gateway.property = ProviderOutcome{Outcome: OutcomeFailed}
	got, err := fx.svc.StartForOwner(context.Background(), fx.owner, fx.listing, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed {
		t.Fatalf("status = %s", got.Status)
	}
	ok, err := fx.svc.IsVerified(context.Background(), fx.listing, TypeProperty)
	if err != nil || ok {
		t.Fatal("failed must not be eligible")
	}
	fx.gateway.property = ProviderOutcome{Outcome: OutcomeVerified}
	next, err := fx.svc.StartForOwner(context.Background(), fx.owner, fx.listing, "")
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == got.ID {
		t.Fatal("failed is terminal; new attempt required")
	}
	if !next.Status.PublishEligible() {
		t.Fatalf("new attempt status = %s", next.Status)
	}

	expiredStore := newFixture(t)
	expiredStore.policy.req = mdcontracts.EIDSRequirementProperty
	exp := expiredStore.now.Add(time.Minute)
	expiredStore.gateway.property = ProviderOutcome{Outcome: OutcomeVerified, ExpiresAt: &exp}
	verified, err := expiredStore.svc.StartForOwner(context.Background(), expiredStore.owner, expiredStore.listing, "")
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != StatusVerified {
		t.Fatalf("status = %s", verified.Status)
	}
	expiredStore.now = expiredStore.now.Add(2 * time.Minute)
	ok, err = expiredStore.svc.IsVerified(context.Background(), expiredStore.listing, TypeProperty)
	if err != nil || ok {
		t.Fatalf("expired must not be eligible: %v %v", ok, err)
	}
}

func TestUnrelatedOwnerDeniedAndNonePolicyRejected(t *testing.T) {
	fx := newFixture(t)
	fx.policy.req = mdcontracts.EIDSRequirementProperty
	fx.gateway.property = ProviderOutcome{Outcome: OutcomeVerified}
	stranger := mustEIDSID(t)
	if _, err := fx.svc.StartForOwner(context.Background(), stranger, fx.listing, ""); !errors.Is(err, errNotFound) {
		t.Fatalf("stranger err = %v", err)
	}
	fx.policy.req = mdcontracts.EIDSRequirementNone
	if _, err := fx.svc.StartForOwner(context.Background(), fx.owner, fx.listing, ""); !errors.Is(err, errNotRequired) {
		t.Fatalf("none err = %v", err)
	}
}

func TestGatewayErrorIsUnavailableNotVerified(t *testing.T) {
	fx := newFixture(t)
	fx.policy.req = mdcontracts.EIDSRequirementProperty
	fx.gateway.err = errors.New("timeout")
	got, err := fx.svc.StartForOwner(context.Background(), fx.owner, fx.listing, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusUnavailable || got.Status.PublishEligible() {
		t.Fatalf("status = %s", got.Status)
	}
}

type fixture struct {
	svc      *Service
	store    *MemoryStore
	listings *stubListings
	policy   *stubPolicy
	gateway  *scriptedGateway
	owner    ID
	listing  ID
	now      time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	now := time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC)
	fx := &fixture{
		store:    NewMemoryStore(),
		listings: &stubListings{category: mustEIDSID(t), owner: mustEIDSID(t)},
		policy:   &stubPolicy{req: mdcontracts.EIDSRequirementNone},
		gateway:  &scriptedGateway{},
		now:      now,
	}
	fx.owner = fx.listings.owner
	fx.listing = mustEIDSID(t)
	fx.listings.listing = fx.listing
	svc, err := NewService(fx.store, fx.listings, fx.policy, fx.gateway, func() time.Time { return fx.now })
	if err != nil {
		t.Fatal(err)
	}
	fx.svc = svc
	return fx
}

type stubListings struct {
	listing  ID
	owner    ID
	category ID
}

func (s *stubListings) ResolveListingEIDSSubject(_ context.Context, listingID listingcontracts.ID) (listingcontracts.ListingEIDSSubject, error) {
	if listingcontracts.ID(s.listing) != listingID {
		return listingcontracts.ListingEIDSSubject{}, listingcontracts.ErrNotFound
	}
	return listingcontracts.ListingEIDSSubject{
		ID:          listingID,
		OwnerUserID: listingcontracts.ID(s.owner),
		CategoryID:  listingcontracts.ID(s.category),
	}, nil
}

type stubPolicy struct {
	req mdcontracts.EIDSRequirement
	err error
}

func (s *stubPolicy) Requirement(context.Context, mdcontracts.ID) (mdcontracts.EIDSRequirement, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.req, nil
}

type scriptedGateway struct {
	property      ProviderOutcome
	vehicle       ProviderOutcome
	err           error
	propertyCalls int
	vehicleCalls  int
}

func (s *scriptedGateway) VerifyProperty(context.Context, PropertyVerifyRequest) (ProviderOutcome, error) {
	s.propertyCalls++
	if s.err != nil {
		return ProviderOutcome{}, s.err
	}
	if s.property.Outcome == "" {
		return ProviderOutcome{Outcome: OutcomeUnavailable}, nil
	}
	return s.property, nil
}

func (s *scriptedGateway) VerifyVehicle(context.Context, VehicleVerifyRequest) (ProviderOutcome, error) {
	s.vehicleCalls++
	if s.err != nil {
		return ProviderOutcome{}, s.err
	}
	if s.vehicle.Outcome == "" {
		return ProviderOutcome{Outcome: OutcomeUnavailable}, nil
	}
	return s.vehicle, nil
}
