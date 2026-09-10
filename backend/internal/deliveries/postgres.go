package deliveries

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/db"
)

var _ store = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

const deliverySelectCols = `id, transaction_id, requester_user_id, provider_user_id,
	eligibility, status, method, note,
	created_at, updated_at, dispatched_at, delivered_at, cancelled_at`

func (p *PostgresStore) Create(ctx context.Context, d Delivery) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := d.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO deliveries.deliveries (
			id, transaction_id, requester_user_id, provider_user_id,
			eligibility, status, method, note,
			created_at, updated_at, dispatched_at, delivered_at, cancelled_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		d.ID, d.TransactionID, d.RequesterUserID, d.ProviderUserID,
		d.Eligibility, string(d.Status), methodString(d.Method), d.Note,
		d.CreatedAt, d.UpdatedAt, d.DispatchedAt, d.DeliveredAt, d.CancelledAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, id ID) (Delivery, error) {
	if p == nil || p.db == nil {
		return Delivery{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+deliverySelectCols+`
		FROM deliveries.deliveries
		WHERE id = $1`, id)
	got, err := scanDelivery(row)
	if err != nil {
		return Delivery{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) GetByTransactionID(ctx context.Context, transactionID ID) (Delivery, error) {
	if p == nil || p.db == nil {
		return Delivery{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+deliverySelectCols+`
		FROM deliveries.deliveries
		WHERE transaction_id = $1`, transactionID)
	got, err := scanDelivery(row)
	if err != nil {
		return Delivery{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) ListForParticipant(ctx context.Context, userID ID) ([]Delivery, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+deliverySelectCols+`
		FROM deliveries.deliveries
		WHERE requester_user_id = $1 OR provider_user_id = $1
		ORDER BY created_at DESC, id ASC`, userID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Delivery, 0)
	for rows.Next() {
		got, err := scanDelivery(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, got)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) Update(ctx context.Context, d Delivery, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := d.Validate(); err != nil {
		return err
	}
	n, err := p.db.Exec(ctx, `
		UPDATE deliveries.deliveries SET
			status = $2,
			updated_at = $3,
			dispatched_at = $4,
			delivered_at = $5,
			cancelled_at = $6
		WHERE id = $1 AND updated_at = $7`,
		d.ID, string(d.Status), d.UpdatedAt, d.DispatchedAt, d.DeliveredAt, d.CancelledAt, expectedUpdatedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		_, getErr := p.Get(ctx, d.ID)
		if errors.Is(getErr, errNotFound) {
			return errNotFound
		}
		if getErr != nil {
			return getErr
		}
		return errConflict
	}
	return nil
}

func scanDelivery(row interface {
	Scan(dest ...any) error
}) (Delivery, error) {
	var d Delivery
	var status string
	var method *string
	if err := row.Scan(
		&d.ID, &d.TransactionID, &d.RequesterUserID, &d.ProviderUserID,
		&d.Eligibility, &status, &method, &d.Note,
		&d.CreatedAt, &d.UpdatedAt, &d.DispatchedAt, &d.DeliveredAt, &d.CancelledAt,
	); err != nil {
		return Delivery{}, err
	}
	d.Status = Status(status)
	if method != nil {
		m := Method(*method)
		d.Method = &m
	}
	return d, nil
}

func methodString(m *Method) *string {
	if m == nil {
		return nil
	}
	v := string(*m)
	return &v
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, db.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
