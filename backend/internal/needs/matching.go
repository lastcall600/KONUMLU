package needs

import (
	"context"
	"errors"

	bizcontracts "backend/internal/businesses/contracts"
	needcontracts "backend/internal/needs/contracts"
)

const (
	// DefaultMatchRadiusKm is used when Need.radiusKm is absent.
	// Conservative V1 default; clients cannot override it via query string.
	DefaultMatchRadiusKm  = needcontracts.DefaultMatchRadiusKm
	DefaultCandidateLimit = 20
	MaxCandidateLimit     = 50
)

type ServiceCandidate struct {
	BusinessID          ID
	BusinessDisplayName string
	ServiceID           ID
	ServiceTitle        string
	ServiceDescription  string
	PriceModel          string
	PriceAmount         *string
	PriceCurrency       *string
	DistanceKm          float64
	CategoryID          *ID
}

func (s *Service) ListCandidates(ctx context.Context, requesterUserID, needID ID, limit int) ([]ServiceCandidate, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if s.candidates == nil {
		return nil, errUnavailable
	}
	need, err := s.GetOwned(ctx, requesterUserID, needID)
	if err != nil {
		return nil, err
	}
	if need.Status != StatusOpen {
		return nil, errInvalidTransition
	}
	limit = clampCandidateLimit(limit)
	radius := DefaultMatchRadiusKm
	if need.RadiusKm != nil {
		radius = *need.RadiusKm
	}
	query := bizcontracts.CandidateQuery{
		Latitude:  need.Latitude,
		Longitude: need.Longitude,
		RadiusKm:  radius,
		Limit:     limit,
	}
	if need.CategoryID != nil {
		id := bizcontracts.ID(*need.CategoryID)
		query.CategoryID = &id
	}
	found, err := s.candidates.FindServiceCandidates(ctx, query)
	if err != nil {
		return nil, mapCandidateErr(err)
	}
	out := make([]ServiceCandidate, 0, len(found))
	seen := make(map[ID]struct{}, len(found))
	for _, row := range found {
		svcID := ID(row.ServiceID)
		if _, ok := seen[svcID]; ok {
			continue
		}
		seen[svcID] = struct{}{}
		out = append(out, fromContractCandidate(row))
	}
	return out, nil
}

func clampCandidateLimit(limit int) int {
	if limit <= 0 {
		return DefaultCandidateLimit
	}
	if limit > MaxCandidateLimit {
		return MaxCandidateLimit
	}
	return limit
}

func fromContractCandidate(row bizcontracts.ServiceCandidate) ServiceCandidate {
	c := ServiceCandidate{
		BusinessID:          ID(row.BusinessID),
		BusinessDisplayName: row.BusinessDisplayName,
		ServiceID:           ID(row.ServiceID),
		ServiceTitle:        row.ServiceTitle,
		ServiceDescription:  row.ServiceDescription,
		PriceModel:          row.PriceModel,
		PriceAmount:         row.PriceAmount,
		PriceCurrency:       row.PriceCurrency,
		DistanceKm:          row.DistanceKm,
	}
	if row.CategoryID != nil {
		id := ID(*row.CategoryID)
		c.CategoryID = &id
	}
	return c
}

func mapCandidateErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, bizcontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, bizcontracts.ErrNotFound) {
		return errNotFound
	}
	if errors.Is(err, bizcontracts.ErrForbidden) {
		return errForbidden
	}
	return errUnavailable
}
