package trust

import (
	"context"
	"time"
)

type projectionStore interface {
	ApplyCompleted(ctx context.Context, event CompletedInteraction, policy LevelPolicy, now time.Time) error
	ApplyVerifiedReview(ctx context.Context, event VerifiedReview, now time.Time) error
	GetProfile(ctx context.Context, userID ID) (UserProfile, error)
	ListHistory(ctx context.Context, userID ID) ([]HistoryEntry, error)
}
