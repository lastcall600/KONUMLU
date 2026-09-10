package payments

import (
	"context"
	"time"
)

type store interface {
	Create(ctx context.Context, p Payment) error
	Get(ctx context.Context, id ID) (Payment, error)
	GetByTransactionID(ctx context.Context, transactionID ID) (Payment, error)
	ListForParticipant(ctx context.Context, userID ID) ([]Payment, error)
	Update(ctx context.Context, p Payment, expectedUpdatedAt time.Time) error
}
