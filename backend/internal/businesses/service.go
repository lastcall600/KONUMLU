package businesses

import (
	"context"
	"errors"
	"time"

	"backend/internal/businesses/contracts"
	mdcontracts "backend/internal/masterdata/contracts"
	"backend/internal/platform/db"
)

type Service struct {
	store      store
	categories mdcontracts.PublishedCategoryLookup
	now        func() time.Time
}

func NewService(store store, categories mdcontracts.PublishedCategoryLookup, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, categories: categories, now: now}, nil
}

func (s *Service) Create(ctx context.Context, ownerUserID ID, content ProfileContent) (Profile, error) {
	if s == nil || s.store == nil {
		return Profile{}, errStoreRequired
	}
	if ownerUserID.IsZero() {
		return Profile{}, errZeroID
	}
	_, err := s.store.GetByOwner(ctx, ownerUserID)
	if err == nil {
		return Profile{}, errConflict
	}
	if !errors.Is(err, errNotFound) {
		return Profile{}, mapStoreErr(err)
	}
	profile, err := NewDraft(ownerUserID, content, s.now().UTC())
	if err != nil {
		return Profile{}, err
	}
	if err := s.store.Create(ctx, profile); err != nil {
		return Profile{}, mapStoreErr(err)
	}
	return profile, nil
}

func (s *Service) Get(ctx context.Context, id ID) (Profile, error) {
	if s == nil || s.store == nil {
		return Profile{}, errStoreRequired
	}
	if id.IsZero() {
		return Profile{}, errZeroID
	}
	profile, err := s.store.Get(ctx, id)
	if err != nil {
		return Profile{}, mapStoreErr(err)
	}
	return profile, nil
}

func (s *Service) GetMine(ctx context.Context, ownerUserID ID) (Profile, error) {
	if s == nil || s.store == nil {
		return Profile{}, errStoreRequired
	}
	if ownerUserID.IsZero() {
		return Profile{}, errZeroID
	}
	profile, err := s.store.GetByOwner(ctx, ownerUserID)
	if err != nil {
		return Profile{}, mapStoreErr(err)
	}
	return profile, nil
}

func (s *Service) GetOwned(ctx context.Context, ownerUserID, businessID ID) (Profile, error) {
	profile, err := s.Get(ctx, businessID)
	if err != nil {
		return Profile{}, err
	}
	if profile.OwnerUserID != ownerUserID {
		return Profile{}, errNotFound
	}
	return profile, nil
}

func (s *Service) GetPublic(ctx context.Context, businessID ID) (Profile, error) {
	profile, err := s.Get(ctx, businessID)
	if err != nil {
		return Profile{}, err
	}
	if !profile.PubliclyReadable() {
		return Profile{}, errNotFound
	}
	return profile, nil
}

func (s *Service) Update(ctx context.Context, ownerUserID, businessID ID, content ProfileContent) (Profile, error) {
	return s.mutateOwned(ctx, ownerUserID, businessID, func(profile Profile, now time.Time) (Profile, error) {
		return profile.UpdateContent(content, now)
	})
}

func (s *Service) UpdateLocation(ctx context.Context, ownerUserID, businessID ID, loc *Coordinates) (Profile, error) {
	return s.mutateOwned(ctx, ownerUserID, businessID, func(profile Profile, now time.Time) (Profile, error) {
		return profile.SetLocation(loc, now)
	})
}

func (s *Service) Activate(ctx context.Context, ownerUserID, businessID ID) (Profile, error) {
	return s.mutateOwned(ctx, ownerUserID, businessID, func(profile Profile, now time.Time) (Profile, error) {
		return profile.Activate(now)
	})
}

func (s *Service) Close(ctx context.Context, ownerUserID, businessID ID) (Profile, error) {
	return s.mutateOwned(ctx, ownerUserID, businessID, func(profile Profile, now time.Time) (Profile, error) {
		return profile.Close(now)
	})
}

func (s *Service) mutateOwned(ctx context.Context, ownerUserID, businessID ID, fn func(Profile, time.Time) (Profile, error)) (Profile, error) {
	if s == nil || s.store == nil {
		return Profile{}, errStoreRequired
	}
	current, err := s.GetOwned(ctx, ownerUserID, businessID)
	if err != nil {
		return Profile{}, err
	}
	next, err := fn(current, s.now().UTC())
	if err != nil {
		return Profile{}, err
	}
	if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
		return Profile{}, mapStoreErr(err)
	}
	return next, nil
}

func (s *Service) CreateOfferedService(ctx context.Context, ownerUserID, businessID ID, content ServiceContent) (OfferedService, error) {
	if s == nil || s.store == nil {
		return OfferedService{}, errStoreRequired
	}
	parent, err := s.GetOwned(ctx, ownerUserID, businessID)
	if err != nil {
		return OfferedService{}, err
	}
	if !parentAllowsServiceMutation(parent.Status) {
		return OfferedService{}, errInvalidTransition
	}
	if err := s.validateCategory(ctx, content.CategoryID); err != nil {
		return OfferedService{}, err
	}
	svc, err := NewDraftOfferedService(parent.ID, content, s.now().UTC())
	if err != nil {
		return OfferedService{}, err
	}
	if err := s.store.CreateOfferedService(ctx, svc); err != nil {
		return OfferedService{}, mapStoreErr(err)
	}
	return svc, nil
}

func (s *Service) GetOwnedOfferedService(ctx context.Context, ownerUserID, businessID, serviceID ID) (OfferedService, error) {
	if _, err := s.GetOwned(ctx, ownerUserID, businessID); err != nil {
		return OfferedService{}, err
	}
	svc, err := s.getOfferedService(ctx, serviceID)
	if err != nil {
		return OfferedService{}, err
	}
	if svc.BusinessID != businessID {
		return OfferedService{}, errNotFound
	}
	return svc, nil
}

func (s *Service) ListOwnedOfferedServices(ctx context.Context, ownerUserID, businessID ID) ([]OfferedService, error) {
	if _, err := s.GetOwned(ctx, ownerUserID, businessID); err != nil {
		return nil, err
	}
	return s.listOfferedServices(ctx, businessID)
}

func (s *Service) GetPublicOfferedService(ctx context.Context, serviceID ID) (OfferedService, error) {
	svc, err := s.getOfferedService(ctx, serviceID)
	if err != nil {
		return OfferedService{}, err
	}
	if !svc.PubliclyReadable() {
		return OfferedService{}, errNotFound
	}
	if _, err := s.GetPublic(ctx, svc.BusinessID); err != nil {
		return OfferedService{}, err
	}
	return svc, nil
}

func (s *Service) ListPublicOfferedServices(ctx context.Context, businessID ID) ([]OfferedService, error) {
	if _, err := s.GetPublic(ctx, businessID); err != nil {
		return nil, err
	}
	all, err := s.listOfferedServices(ctx, businessID)
	if err != nil {
		return nil, err
	}
	out := make([]OfferedService, 0, len(all))
	for _, svc := range all {
		if svc.PubliclyReadable() {
			out = append(out, svc)
		}
	}
	return out, nil
}

func (s *Service) UpdateOfferedService(ctx context.Context, ownerUserID, businessID, serviceID ID, content ServiceContent) (OfferedService, error) {
	if err := s.validateCategory(ctx, content.CategoryID); err != nil {
		return OfferedService{}, err
	}
	return s.mutateOwnedOfferedService(ctx, ownerUserID, businessID, serviceID, func(svc OfferedService, now time.Time) (OfferedService, error) {
		return svc.UpdateContent(content, now)
	})
}

func (s *Service) ActivateOfferedService(ctx context.Context, ownerUserID, businessID, serviceID ID) (OfferedService, error) {
	return s.mutateOwnedOfferedService(ctx, ownerUserID, businessID, serviceID, func(svc OfferedService, now time.Time) (OfferedService, error) {
		return svc.Activate(now)
	})
}

func (s *Service) PauseOfferedService(ctx context.Context, ownerUserID, businessID, serviceID ID) (OfferedService, error) {
	return s.mutateOwnedOfferedService(ctx, ownerUserID, businessID, serviceID, func(svc OfferedService, now time.Time) (OfferedService, error) {
		return svc.Pause(now)
	})
}

func (s *Service) CloseOfferedService(ctx context.Context, ownerUserID, businessID, serviceID ID) (OfferedService, error) {
	return s.mutateOwnedOfferedService(ctx, ownerUserID, businessID, serviceID, func(svc OfferedService, now time.Time) (OfferedService, error) {
		return svc.Close(now)
	})
}

func (s *Service) mutateOwnedOfferedService(ctx context.Context, ownerUserID, businessID, serviceID ID, fn func(OfferedService, time.Time) (OfferedService, error)) (OfferedService, error) {
	if s == nil || s.store == nil {
		return OfferedService{}, errStoreRequired
	}
	parent, err := s.GetOwned(ctx, ownerUserID, businessID)
	if err != nil {
		return OfferedService{}, err
	}
	if !parentAllowsServiceMutation(parent.Status) {
		return OfferedService{}, errInvalidTransition
	}
	current, err := s.getOfferedService(ctx, serviceID)
	if err != nil {
		return OfferedService{}, err
	}
	if current.BusinessID != businessID {
		return OfferedService{}, errNotFound
	}
	next, err := fn(current, s.now().UTC())
	if err != nil {
		return OfferedService{}, err
	}
	if err := s.store.UpdateOfferedService(ctx, next, current.UpdatedAt); err != nil {
		return OfferedService{}, mapStoreErr(err)
	}
	return next, nil
}

func (s *Service) getOfferedService(ctx context.Context, id ID) (OfferedService, error) {
	if s == nil || s.store == nil {
		return OfferedService{}, errStoreRequired
	}
	if id.IsZero() {
		return OfferedService{}, errZeroID
	}
	svc, err := s.store.GetOfferedService(ctx, id)
	if err != nil {
		return OfferedService{}, mapStoreErr(err)
	}
	return svc, nil
}

func (s *Service) listOfferedServices(ctx context.Context, businessID ID) ([]OfferedService, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if businessID.IsZero() {
		return nil, errZeroID
	}
	list, err := s.store.ListOfferedServices(ctx, businessID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	if list == nil {
		return []OfferedService{}, nil
	}
	return list, nil
}

func (s *Service) FindServiceCandidates(ctx context.Context, query contracts.CandidateQuery) ([]contracts.ServiceCandidate, error) {
	if s == nil || s.store == nil {
		return nil, contracts.ErrUnavailable
	}
	if err := ValidateCoordinates(Coordinates{Latitude: query.Latitude, Longitude: query.Longitude}); err != nil {
		return nil, mapLookupErr(err)
	}
	if query.RadiusKm <= 0 {
		return nil, mapLookupErr(errInvalidRadius)
	}
	if query.CategoryID != nil && query.CategoryID.IsZero() {
		return nil, contracts.ErrZeroID
	}
	if query.Limit <= 0 {
		return []contracts.ServiceCandidate{}, nil
	}
	list, err := s.store.FindServiceCandidates(ctx, query)
	if err != nil {
		return nil, mapLookupErr(err)
	}
	if list == nil {
		return []contracts.ServiceCandidate{}, nil
	}
	return list, nil
}

func (s *Service) CheckServiceCandidate(ctx context.Context, check contracts.EligibilityCheck) (contracts.ServiceCandidate, error) {
	if s == nil || s.store == nil {
		return contracts.ServiceCandidate{}, contracts.ErrUnavailable
	}
	if check.BusinessID.IsZero() || check.ServiceID.IsZero() {
		return contracts.ServiceCandidate{}, contracts.ErrZeroID
	}
	if err := ValidateCoordinates(Coordinates{Latitude: check.Latitude, Longitude: check.Longitude}); err != nil {
		return contracts.ServiceCandidate{}, mapLookupErr(err)
	}
	if check.RadiusKm <= 0 {
		return contracts.ServiceCandidate{}, mapLookupErr(errInvalidRadius)
	}
	if check.CategoryID != nil && check.CategoryID.IsZero() {
		return contracts.ServiceCandidate{}, contracts.ErrZeroID
	}
	row, err := s.store.CheckServiceCandidate(ctx, check)
	if err != nil {
		return contracts.ServiceCandidate{}, mapLookupErr(err)
	}
	return row, nil
}

func (s *Service) validateCategory(ctx context.Context, categoryID *ID) error {
	if categoryID == nil {
		return nil
	}
	if categoryID.IsZero() {
		return errZeroID
	}
	if s.categories == nil {
		return errUnavailable
	}
	if err := s.categories.RequirePublished(ctx, mdcontracts.ID(*categoryID)); err != nil {
		if errors.Is(err, mdcontracts.ErrZeroID) {
			return errZeroID
		}
		if errors.Is(err, mdcontracts.ErrNotFound) {
			return errInvalidCategory
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return errUnavailable
	}
	return nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, errConflict) || errors.Is(err, db.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, db.ErrUnavailable) {
		return errUnavailable
	}
	if errors.Is(err, errStoreRequired) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidBusiness) || errors.Is(err, errInvalidStatus) ||
		errors.Is(err, errInvalidTransition) || errors.Is(err, errInvalidContent) ||
		errors.Is(err, errForbidden) || errors.Is(err, errInvalidOfferedService) ||
		errors.Is(err, errInvalidPrice) || errors.Is(err, errInvalidLocation) ||
		errors.Is(err, errInvalidLatitude) || errors.Is(err, errInvalidLongitude) ||
		errors.Is(err, errInvalidCategory) || errors.Is(err, errInvalidRadius) {
		return err
	}
	return errUnavailable
}

func mapLookupErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) {
		return contracts.ErrZeroID
	}
	if errors.Is(err, errNotFound) {
		return contracts.ErrNotFound
	}
	if errors.Is(err, errForbidden) {
		return contracts.ErrForbidden
	}
	if errors.Is(err, errInvalidLocation) || errors.Is(err, errInvalidLatitude) ||
		errors.Is(err, errInvalidLongitude) || errors.Is(err, errInvalidRadius) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return contracts.ErrUnavailable
}
