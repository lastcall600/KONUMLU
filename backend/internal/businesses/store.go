package businesses

import (
	"context"
	"time"

	"backend/internal/businesses/contracts"
)

type profileStore interface {
	Create(ctx context.Context, profile Profile) error
	Get(ctx context.Context, id ID) (Profile, error)
	GetByOwner(ctx context.Context, ownerUserID ID) (Profile, error)
	Update(ctx context.Context, profile Profile, expectedUpdatedAt time.Time) error
}

type offeredServiceStore interface {
	CreateOfferedService(ctx context.Context, svc OfferedService) error
	GetOfferedService(ctx context.Context, id ID) (OfferedService, error)
	ListOfferedServices(ctx context.Context, businessID ID) ([]OfferedService, error)
	UpdateOfferedService(ctx context.Context, svc OfferedService, expectedUpdatedAt time.Time) error
}

type store interface {
	profileStore
	offeredServiceStore
	candidateStore
}

type candidateStore interface {
	FindServiceCandidates(ctx context.Context, query contracts.CandidateQuery) ([]contracts.ServiceCandidate, error)
	CheckServiceCandidate(ctx context.Context, check contracts.EligibilityCheck) (contracts.ServiceCandidate, error)
}
