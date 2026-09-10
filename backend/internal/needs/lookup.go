package needs

import (
	"context"
	"errors"

	"backend/internal/needs/contracts"
)

var _ contracts.Lookup = (*Service)(nil)

func (s *Service) GetNeed(ctx context.Context, needID contracts.ID) (contracts.NeedRef, error) {
	need, err := s.get(ctx, ID(needID))
	if err != nil {
		return contracts.NeedRef{}, mapNeedLookupErr(err)
	}
	return toNeedRef(need), nil
}

func (s *Service) AssertOwnedBy(ctx context.Context, needID, userID contracts.ID) error {
	if needID.IsZero() || userID.IsZero() {
		return contracts.ErrZeroID
	}
	ref, err := s.GetNeed(ctx, needID)
	if err != nil {
		return err
	}
	if ref.RequesterUserID != userID {
		return contracts.ErrNotFound
	}
	return nil
}

func toNeedRef(n Need) contracts.NeedRef {
	ref := contracts.NeedRef{
		ID:              contracts.ID(n.ID),
		RequesterUserID: contracts.ID(n.RequesterUserID),
		Status:          string(n.Status),
		Title:           n.Title,
		Description:     n.Description,
		Latitude:        n.Latitude,
		Longitude:       n.Longitude,
		RadiusKm:        cloneRadius(n.RadiusKm),
		ExpiresAt:       cloneTime(n.ExpiresAt),
	}
	if n.CategoryID != nil {
		id := contracts.ID(*n.CategoryID)
		ref.CategoryID = &id
	}
	if n.Budget != nil {
		ref.Budget = &contracts.Budget{
			MinAmount: n.Budget.MinAmount,
			MaxAmount: n.Budget.MaxAmount,
			Currency:  n.Budget.Currency,
		}
	}
	return ref
}

func mapNeedLookupErr(err error) error {
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
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return contracts.ErrUnavailable
}
