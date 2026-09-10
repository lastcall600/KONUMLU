package deliveries

import (
	"context"
	"time"
)

type store interface {
	Create(ctx context.Context, d Delivery) error
	Get(ctx context.Context, id ID) (Delivery, error)
	GetByTransactionID(ctx context.Context, transactionID ID) (Delivery, error)
	ListForParticipant(ctx context.Context, userID ID) ([]Delivery, error)
	Update(ctx context.Context, d Delivery, expectedUpdatedAt time.Time) error
}
