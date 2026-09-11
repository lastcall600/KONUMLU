package trust

import "context"

// Service reads public-safe self trust profiles from the derived projection.
type Service struct {
	projector *Projector
}

func NewService(projector *Projector) (*Service, error) {
	if projector == nil {
		return nil, errStoreRequired
	}
	return &Service{projector: projector}, nil
}

func (s *Service) GetMe(ctx context.Context, userID ID) (UserProfile, error) {
	if s == nil || s.projector == nil {
		return UserProfile{}, errStoreRequired
	}
	return s.projector.Profile(ctx, userID)
}

// GetPublic reads the derived projection by internal user id.
// Public HTTP must obtain that id only via Identity public-profile resolution.
func (s *Service) GetPublic(ctx context.Context, userID ID) (PublicPassport, error) {
	profile, err := s.GetMe(ctx, userID)
	if err != nil {
		return PublicPassport{}, err
	}
	return ToPublicPassport(profile), nil
}

func (s *Service) History(ctx context.Context, userID ID) ([]HistoryEntry, error) {
	if s == nil || s.projector == nil {
		return nil, errStoreRequired
	}
	return s.projector.History(ctx, userID)
}
