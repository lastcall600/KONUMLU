package transactions

import (
	"context"
	"time"
)

type store interface {
	Create(ctx context.Context, txn Transaction) error
	Get(ctx context.Context, id ID) (Transaction, error)
	GetByOfferID(ctx context.Context, offerID ID) (Transaction, error)
	ListForParticipant(ctx context.Context, userID ID) ([]Transaction, error)
	Update(ctx context.Context, txn Transaction, expectedUpdatedAt time.Time) error
}
