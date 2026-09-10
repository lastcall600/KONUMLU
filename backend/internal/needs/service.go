package needs

import (
	"context"
	"errors"
	"time"

	bizcontracts "backend/internal/businesses/contracts"
	mdcontracts "backend/internal/masterdata/contracts"
	"backend/internal/platform/db"
)

type Service struct {
	store      store
	categories mdcontracts.PublishedCategoryLookup
	candidates bizcontracts.CandidateDiscovery
	now        func() time.Time
}

func NewService(store store, categories mdcontracts.PublishedCategoryLookup, candidates bizcontracts.CandidateDiscovery, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, categories: categories, candidates: candidates, now: now}, nil
}

func (s *Service) Create(ctx context.Context, requesterUserID ID, content Content) (Need, error) {
	if s == nil || s.store == nil {
		return Need{}, errStoreRequired
	}
	if requesterUserID.IsZero() {
		return Need{}, errZeroID
	}
	if err := s.validateCategory(ctx, content.CategoryID); err != nil {
		return Need{}, err
	}
	need, err := NewDraft(requesterUserID, content, s.now().UTC())
	if err != nil {
		return Need{}, err
	}
	if err := s.store.Create(ctx, need); err != nil {
		return Need{}, mapStoreErr(err)
	}
	return need, nil
}

func (s *Service) GetOwned(ctx context.Context, requesterUserID, needID ID) (Need, error) {
	need, err := s.get(ctx, needID)
	if err != nil {
		return Need{}, err
	}
	if need.RequesterUserID != requesterUserID {
		return Need{}, errNotFound
	}
	return need, nil
}

func (s *Service) ListOwned(ctx context.Context, requesterUserID ID) ([]Need, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if requesterUserID.IsZero() {
		return nil, errZeroID
	}
	list, err := s.store.ListByRequester(ctx, requesterUserID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	if list == nil {
		return []Need{}, nil
	}
	return list, nil
}

func (s *Service) Update(ctx context.Context, requesterUserID, needID ID, content Content) (Need, error) {
	if err := s.validateCategory(ctx, content.CategoryID); err != nil {
		return Need{}, err
	}
	return s.mutateOwned(ctx, requesterUserID, needID, func(need Need, now time.Time) (Need, error) {
		return need.UpdateContent(content, now)
	})
}

func (s *Service) Open(ctx context.Context, requesterUserID, needID ID) (Need, error) {
	return s.mutateOwned(ctx, requesterUserID, needID, func(need Need, now time.Time) (Need, error) {
		return need.Open(now)
	})
}

func (s *Service) Fulfill(ctx context.Context, requesterUserID, needID ID) (Need, error) {
	return s.mutateOwned(ctx, requesterUserID, needID, func(need Need, now time.Time) (Need, error) {
		return need.Fulfill(now)
	})
}

func (s *Service) Cancel(ctx context.Context, requesterUserID, needID ID) (Need, error) {
	return s.mutateOwned(ctx, requesterUserID, needID, func(need Need, now time.Time) (Need, error) {
		return need.Cancel(now)
	})
}

// ExpireIfDue applies server-side expiry. It is not an owner HTTP action.
func (s *Service) ExpireIfDue(ctx context.Context, needID ID) (Need, error) {
	if s == nil || s.store == nil {
		return Need{}, errStoreRequired
	}
	current, err := s.get(ctx, needID)
	if err != nil {
		return Need{}, err
	}
	next, err := current.Expire(s.now().UTC())
	if err != nil {
		return Need{}, err
	}
	if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
		return Need{}, mapStoreErr(err)
	}
	return next, nil
}

func (s *Service) mutateOwned(ctx context.Context, requesterUserID, needID ID, fn func(Need, time.Time) (Need, error)) (Need, error) {
	if s == nil || s.store == nil {
		return Need{}, errStoreRequired
	}
	current, err := s.GetOwned(ctx, requesterUserID, needID)
	if err != nil {
		return Need{}, err
	}
	next, err := fn(current, s.now().UTC())
	if err != nil {
		return Need{}, err
	}
	if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
		return Need{}, mapStoreErr(err)
	}
	return next, nil
}

func (s *Service) get(ctx context.Context, id ID) (Need, error) {
	if s == nil || s.store == nil {
		return Need{}, errStoreRequired
	}
	if id.IsZero() {
		return Need{}, errZeroID
	}
	need, err := s.store.Get(ctx, id)
	if err != nil {
		return Need{}, mapStoreErr(err)
	}
	return need, nil
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
		errors.Is(err, errInvalidNeed) || errors.Is(err, errInvalidStatus) ||
		errors.Is(err, errInvalidTransition) || errors.Is(err, errInvalidContent) ||
		errors.Is(err, errInvalidLocation) || errors.Is(err, errInvalidLatitude) ||
		errors.Is(err, errInvalidLongitude) || errors.Is(err, errInvalidBudget) ||
		errors.Is(err, errInvalidRadius) || errors.Is(err, errInvalidExpiry) ||
		errors.Is(err, errInvalidCategory) || errors.Is(err, errForbidden) {
		return err
	}
	return errUnavailable
}
