package eids

import (
	"context"
	"time"
)

type store interface {
	Create(ctx context.Context, v Verification) error
	Get(ctx context.Context, id ID) (Verification, error)
	GetOpen(ctx context.Context, listingID ID, kind VerificationType) (Verification, error)
	GetLatest(ctx context.Context, listingID ID, kind VerificationType) (Verification, error)
	Update(ctx context.Context, v Verification, expectedUpdatedAt time.Time) error
}
