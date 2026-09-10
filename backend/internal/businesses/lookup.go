package businesses

import (
	"context"

	"backend/internal/businesses/contracts"
)

var _ contracts.Lookup = (*Service)(nil)

func (s *Service) GetBusiness(ctx context.Context, businessID contracts.ID) (contracts.ProfileRef, error) {
	profile, err := s.Get(ctx, ID(businessID))
	if err != nil {
		return contracts.ProfileRef{}, mapLookupErr(err)
	}
	return contracts.ProfileRef{
		ID:          contracts.ID(profile.ID),
		OwnerUserID: contracts.ID(profile.OwnerUserID),
		Status:      string(profile.Status),
		DisplayName: profile.DisplayName,
	}, nil
}

func (s *Service) AssertOwnedBy(ctx context.Context, businessID, userID contracts.ID) error {
	if businessID.IsZero() || userID.IsZero() {
		return contracts.ErrZeroID
	}
	ref, err := s.GetBusiness(ctx, businessID)
	if err != nil {
		return err
	}
	if ref.OwnerUserID != userID {
		return contracts.ErrForbidden
	}
	return nil
}

var _ contracts.Catalog = (*Service)(nil)
var _ contracts.CandidateDiscovery = (*Service)(nil)
var _ contracts.OfferEligibility = (*Service)(nil)

func (s *Service) GetService(ctx context.Context, serviceID contracts.ID) (contracts.OfferedServiceRef, error) {
	svc, err := s.getOfferedService(ctx, ID(serviceID))
	if err != nil {
		return contracts.OfferedServiceRef{}, mapLookupErr(err)
	}
	return toServiceRef(svc), nil
}

func (s *Service) ListServicesForBusiness(ctx context.Context, businessID contracts.ID) ([]contracts.OfferedServiceRef, error) {
	if businessID.IsZero() {
		return nil, contracts.ErrZeroID
	}
	if _, err := s.Get(ctx, ID(businessID)); err != nil {
		return nil, mapLookupErr(err)
	}
	list, err := s.listOfferedServices(ctx, ID(businessID))
	if err != nil {
		return nil, mapLookupErr(err)
	}
	out := make([]contracts.OfferedServiceRef, 0, len(list))
	for _, svc := range list {
		out = append(out, toServiceRef(svc))
	}
	return out, nil
}

func toServiceRef(svc OfferedService) contracts.OfferedServiceRef {
	return contracts.OfferedServiceRef{
		ID:         contracts.ID(svc.ID),
		BusinessID: contracts.ID(svc.BusinessID),
		Status:     string(svc.Status),
		Title:      svc.Title,
	}
}
