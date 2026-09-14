package eids

import (
	"context"
	"errors"
	"time"

	"backend/internal/eids/trdecision"
	listingcontracts "backend/internal/listings/contracts"
	mdcontracts "backend/internal/masterdata/contracts"
)

type Service struct {
	store    store
	listings listingcontracts.EIDSSubject
	policy   mdcontracts.EIDSRequirementLookup
	gateway  Gateway
	now      func() time.Time
	verifier *trdecision.Verifier
}

func NewService(store store, listings listingcontracts.EIDSSubject, policy mdcontracts.EIDSRequirementLookup, gateway Gateway, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if gateway == nil {
		return nil, errGatewayRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, listings: listings, policy: policy, gateway: gateway, now: now}, nil
}

func (s *Service) StartForOwner(ctx context.Context, ownerUserID, listingID ID, clientType string) (Verification, error) {
	if s == nil || s.store == nil {
		return Verification{}, errStoreRequired
	}
	kind, err := s.requiredKindForOwner(ctx, ownerUserID, listingID)
	if err != nil {
		return Verification{}, err
	}
	if clientType != "" {
		requested, err := ParseType(clientType)
		if err != nil {
			return Verification{}, err
		}
		if requested != kind {
			return Verification{}, errWrongType
		}
	}
	return s.startKind(ctx, listingID, kind)
}

func (s *Service) CurrentForOwner(ctx context.Context, ownerUserID, listingID ID) (Verification, error) {
	if s == nil || s.store == nil {
		return Verification{}, errStoreRequired
	}
	kind, err := s.requiredKindForOwner(ctx, ownerUserID, listingID)
	if err != nil {
		return Verification{}, err
	}
	v, err := s.latestEffective(ctx, listingID, kind)
	if err != nil {
		return Verification{}, err
	}
	return v, nil
}

func (s *Service) IsVerified(ctx context.Context, listingID ID, kind VerificationType) (bool, error) {
	if s == nil || s.store == nil {
		return false, errStoreRequired
	}
	if listingID.IsZero() {
		return false, errZeroID
	}
	if !kind.valid() {
		return false, errInvalidType
	}
	v, err := s.latestEffective(ctx, listingID, kind)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return false, nil
		}
		return false, err
	}
	return v.Status.PublishEligible(), nil
}

func (s *Service) requiredKindForOwner(ctx context.Context, ownerUserID, listingID ID) (VerificationType, error) {
	if ownerUserID.IsZero() || listingID.IsZero() {
		return "", errZeroID
	}
	if s.listings == nil {
		return "", errUnavailable
	}
	subject, err := s.listings.ResolveListingEIDSSubject(ctx, listingcontracts.ID(listingID))
	if err != nil {
		return "", mapListingErr(err)
	}
	if listingcontracts.ID(ownerUserID) != subject.OwnerUserID {
		return "", errNotFound
	}
	if s.policy == nil {
		return "", errUnavailable
	}
	req, err := s.policy.Requirement(ctx, mdcontracts.ID(subject.CategoryID))
	if err != nil {
		return "", mapPolicyErr(err)
	}
	switch req {
	case mdcontracts.EIDSRequirementNone:
		return "", errNotRequired
	case mdcontracts.EIDSRequirementProperty:
		return TypeProperty, nil
	case mdcontracts.EIDSRequirementVehicle:
		return TypeVehicle, nil
	default:
		return "", errUnavailable
	}
}

func (s *Service) startKind(ctx context.Context, listingID ID, kind VerificationType) (Verification, error) {
	open, err := s.store.GetOpen(ctx, listingID, kind)
	if err == nil {
		return s.retryOrReturn(ctx, open)
	}
	if !errors.Is(err, errNotFound) {
		return Verification{}, mapStoreErr(err)
	}
	latest, err := s.latestEffective(ctx, listingID, kind)
	if err == nil && latest.Status == StatusVerified {
		return latest, nil
	}
	if err != nil && !errors.Is(err, errNotFound) {
		return Verification{}, err
	}
	pending, err := NewPending(listingID, kind, s.now().UTC())
	if err != nil {
		return Verification{}, err
	}
	if err := s.store.Create(ctx, pending); err != nil {
		if errors.Is(err, errConflict) {
			open, getErr := s.store.GetOpen(ctx, listingID, kind)
			if getErr != nil {
				return Verification{}, mapStoreErr(getErr)
			}
			return s.retryOrReturn(ctx, open)
		}
		return Verification{}, mapStoreErr(err)
	}
	if err := s.ensureSubjectRef(ctx, pending); err != nil {
		return Verification{}, mapStoreErr(err)
	}
	return s.invokeGateway(ctx, pending)
}

func (s *Service) retryOrReturn(ctx context.Context, v Verification) (Verification, error) {
	effective, err := s.persistEffective(ctx, v)
	if err != nil {
		return Verification{}, err
	}
	if err := s.ensureSubjectRef(ctx, effective); err != nil {
		return Verification{}, mapStoreErr(err)
	}
	switch effective.Status {
	case StatusPending, StatusUnavailable:
		return s.invokeGateway(ctx, effective)
	case StatusInProgress, StatusVerified:
		return effective, nil
	default:
		return effective, nil
	}
}

func (s *Service) invokeGateway(ctx context.Context, v Verification) (Verification, error) {
	if s.gateway == nil {
		return Verification{}, errGatewayRequired
	}
	var (
		out ProviderOutcome
		err error
	)
	switch v.VerificationType {
	case TypeProperty:
		out, err = s.gateway.VerifyProperty(ctx, PropertyVerifyRequest{VerificationID: v.ID, ListingID: v.ListingID})
	case TypeVehicle:
		out, err = s.gateway.VerifyVehicle(ctx, VehicleVerifyRequest{VerificationID: v.ID, ListingID: v.ListingID})
	default:
		return Verification{}, errInvalidType
	}
	if err != nil || !out.Outcome.valid() {
		out = ProviderOutcome{Outcome: OutcomeUnavailable}
	}
	now := s.now().UTC()
	next, err := v.ApplyOutcome(out, now)
	if err != nil {
		return Verification{}, err
	}
	if err := s.store.Update(ctx, next, v.UpdatedAt); err != nil {
		return Verification{}, mapStoreErr(err)
	}
	return next, nil
}

func (s *Service) latestEffective(ctx context.Context, listingID ID, kind VerificationType) (Verification, error) {
	v, err := s.store.GetLatest(ctx, listingID, kind)
	if err != nil {
		return Verification{}, mapStoreErr(err)
	}
	return s.persistEffective(ctx, v)
}

func (s *Service) persistEffective(ctx context.Context, v Verification) (Verification, error) {
	next, err := v.Effective(s.now().UTC())
	if err != nil {
		return Verification{}, err
	}
	if next.Status == v.Status && next.UpdatedAt.Equal(v.UpdatedAt) {
		return next, nil
	}
	if err := s.store.Update(ctx, next, v.UpdatedAt); err != nil {
		return Verification{}, mapStoreErr(err)
	}
	return next, nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errConflict) || errors.Is(err, errUnavailable) ||
		errors.Is(err, errStoreRequired) || errors.Is(err, errReplayConflict) || errors.Is(err, errUnmappedSubject) ||
		errors.Is(err, errDecisionRejected) || errors.Is(err, errInvalidVerification) || errors.Is(err, errInvalidTransition) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}

func mapListingErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, listingcontracts.ErrNotFound) || errors.Is(err, listingcontracts.ErrForbidden) {
		return errNotFound
	}
	if errors.Is(err, listingcontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}

func mapPolicyErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, mdcontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, mdcontracts.ErrNotFound) || errors.Is(err, mdcontracts.ErrUnavailable) {
		return errUnavailable
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
