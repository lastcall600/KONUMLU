package needs

import (
	"context"
	"time"
)

type store interface {
	Create(ctx context.Context, need Need) error
	Get(ctx context.Context, id ID) (Need, error)
	ListByRequester(ctx context.Context, requesterUserID ID) ([]Need, error)
	Update(ctx context.Context, need Need, expectedUpdatedAt time.Time) error
}
