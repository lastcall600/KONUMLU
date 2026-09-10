package outbox

import (
	"context"
	"time"
)

// Execer runs SQL in a connection or in the caller's existing transaction.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
}

type store interface {
	Insert(ctx context.Context, exec Execer, event Event) error
	Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Event, error)
	Complete(ctx context.Context, id ID, now time.Time) error
	Reschedule(ctx context.Context, id ID, availableAt time.Time, errorClass string) error
	IncrementAttempts(ctx context.Context, id ID) (int, error)
}
