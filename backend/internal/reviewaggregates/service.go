package reviewaggregates

import "context"

// Service reads public-safe derived review summaries.
type Service struct {
	projector *Projector
}

func NewService(projector *Projector) (*Service, error) {
	if projector == nil {
		return nil, errStoreRequired
	}
	return &Service{projector: projector}, nil
}

func (s *Service) ListingAccuracy(ctx context.Context, listingID ID) (RatingSummary, error) {
	if s == nil || s.projector == nil {
		return RatingSummary{}, errStoreRequired
	}
	return s.projector.ListingAccuracy(ctx, listingID)
}

func (s *Service) ProviderServiceMe(ctx context.Context, userID ID) (RatingSummary, error) {
	if s == nil || s.projector == nil {
		return RatingSummary{}, errStoreRequired
	}
	return s.projector.ProviderService(ctx, userID)
}
