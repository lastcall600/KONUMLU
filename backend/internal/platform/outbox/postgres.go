package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/platform/db"
)

var _ store = (*PostgresStore)(nil)

// RETURNING must qualify columns: the CTE `picked` also exposes `id`.
const eventReturningCols = `e.id, e.event_type, e.event_version, e.aggregate_type, e.aggregate_id, e.payload,
	e.idempotency_key, e.correlation_id, e.created_at, e.available_at, e.claimed_at, e.claim_until,
	e.completed_at, e.attempts, e.last_error_class`

const claimSQL = `
WITH picked AS (
	SELECT id
	FROM platform.outbox_events
	WHERE completed_at IS NULL
	  AND available_at <= $1
	  AND (claim_until IS NULL OR claim_until <= $1)
	ORDER BY available_at, id
	FOR UPDATE SKIP LOCKED
	LIMIT $2
)
UPDATE platform.outbox_events e
SET claimed_at = $1,
    claim_until = $3,
    attempts = attempts + 1
FROM picked
WHERE e.id = picked.id
RETURNING ` + eventReturningCols

// PostgresStore persists outbox events. Claim is a single statement so the
// row lock is released before any external I/O.
type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

func (p *PostgresStore) Insert(ctx context.Context, exec Execer, event Event) error {
	if exec == nil {
		if p.db == nil {
			return ErrUnavailable
		}
		exec = p.db
	}
	_, err := exec.Exec(ctx, `
		INSERT INTO platform.outbox_events (
			id, event_type, event_version, aggregate_type, aggregate_id, payload,
			idempotency_key, correlation_id, created_at, available_at, claimed_at, claim_until,
			completed_at, attempts, last_error_class
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		event.ID, event.EventType, event.EventVersion, event.AggregateType, event.AggregateID, []byte(event.Payload),
		event.IdempotencyKey, event.CorrelationID, event.CreatedAt, event.AvailableAt, event.ClaimedAt, event.ClaimUntil,
		event.CompletedAt, event.Attempts, event.LastErrorClass,
	)
	if errors.Is(err, db.ErrConflict) {
		return ErrConflict
	}
	return mapDBErr(err)
}

func (p *PostgresStore) Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Event, error) {
	if p.db == nil {
		return nil, ErrUnavailable
	}
	if lease <= 0 || limit <= 0 {
		return nil, ErrInvalidPolicy
	}
	rows, err := p.db.Query(ctx, claimSQL, now, limit, now.Add(lease))
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()

	out := make([]Event, 0)
	for rows.Next() {
		e, scanErr := scanEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) Complete(ctx context.Context, id ID, now time.Time) error {
	if p.db == nil {
		return ErrUnavailable
	}
	n, err := p.db.Exec(ctx, `
		UPDATE platform.outbox_events
		SET completed_at = $2
		WHERE id = $1 AND completed_at IS NULL`, id, now)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresStore) Reschedule(ctx context.Context, id ID, availableAt time.Time, errorClass string) error {
	if p.db == nil {
		return ErrUnavailable
	}
	n, err := p.db.Exec(ctx, `
		UPDATE platform.outbox_events
		SET available_at = $2,
		    claimed_at = NULL,
		    claim_until = NULL,
		    last_error_class = $3
		WHERE id = $1 AND completed_at IS NULL`, id, availableAt, errorClass)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresStore) IncrementAttempts(ctx context.Context, id ID) (int, error) {
	if p.db == nil {
		return 0, ErrUnavailable
	}
	row := p.db.QueryRow(ctx, `
		UPDATE platform.outbox_events
		SET attempts = attempts + 1
		WHERE id = $1 AND completed_at IS NULL
		RETURNING attempts`, id)
	var n int
	if err := row.Scan(&n); err != nil {
		mapped := mapDBErr(err)
		if errors.Is(mapped, ErrNotFound) {
			return 0, ErrNotFound
		}
		return 0, mapped
	}
	return n, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(row scanner) (Event, error) {
	var e Event
	var payload []byte
	if err := row.Scan(
		&e.ID, &e.EventType, &e.EventVersion, &e.AggregateType, &e.AggregateID, &payload,
		&e.IdempotencyKey, &e.CorrelationID, &e.CreatedAt, &e.AvailableAt, &e.ClaimedAt, &e.ClaimUntil,
		&e.CompletedAt, &e.Attempts, &e.LastErrorClass,
	); err != nil {
		return Event{}, mapDBErr(err)
	}
	e.Payload = json.RawMessage(append([]byte(nil), payload...))
	return e, nil
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return ErrNotFound
	}
	if errors.Is(err, db.ErrConflict) {
		return ErrConflict
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, db.ErrUnavailable) {
		return ErrUnavailable
	}
	return ErrUnavailable
}
