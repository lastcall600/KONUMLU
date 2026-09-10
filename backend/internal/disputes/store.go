package disputes

import (
	"context"
	"time"
)

type store interface {
	Create(ctx context.Context, d Dispute) error
	Get(ctx context.Context, id ID) (Dispute, error)
	GetActiveByTransactionID(ctx context.Context, transactionID ID) (Dispute, error)
	ListConcludedByTransactionID(ctx context.Context, transactionID ID) ([]Dispute, error)
	ListForParticipant(ctx context.Context, userID ID) ([]Dispute, error)
	Update(ctx context.Context, d Dispute, expectedUpdatedAt time.Time) error
	AppendEvidence(ctx context.Context, e Evidence) error
	ListEvidence(ctx context.Context, disputeID ID) ([]Evidence, error)
}
