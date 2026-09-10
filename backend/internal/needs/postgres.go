package needs

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

const needSelectCols = `id, requester_user_id, title, description, category_id, status,
	budget_min_amount::text, budget_max_amount::text, budget_currency,
	ST_Y(point::geometry) AS latitude, ST_X(point::geometry) AS longitude,
	radius_km, created_at, updated_at, expires_at`

func (p *PostgresStore) Create(ctx context.Context, need Need) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := need.Validate(); err != nil {
		return err
	}
	minAmt, maxAmt, currency := budgetArgs(need.Budget)
	_, err := p.db.Exec(ctx, `
		INSERT INTO needs.needs (
			id, requester_user_id, title, description, category_id, status,
			budget_min_amount, budget_max_amount, budget_currency,
			point, radius_km, created_at, updated_at, expires_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,
			ST_SetSRID(ST_MakePoint($10, $11), 4326)::geography,
			$12,$13,$14,$15
		)`,
		need.ID, need.RequesterUserID, need.Title, nullIfEmpty(need.Description), idArg(need.CategoryID),
		string(need.Status), minAmt, maxAmt, currency,
		need.Longitude, need.Latitude, need.RadiusKm,
		need.CreatedAt, need.UpdatedAt, need.ExpiresAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, id ID) (Need, error) {
	if p == nil || p.db == nil {
		return Need{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+needSelectCols+`
		FROM needs.needs
		WHERE id = $1`, id)
	got, err := scanNeed(row)
	if err != nil {
		return Need{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) ListByRequester(ctx context.Context, requesterUserID ID) ([]Need, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+needSelectCols+`
		FROM needs.needs
		WHERE requester_user_id = $1
		ORDER BY created_at DESC, id ASC`, requesterUserID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Need, 0)
	for rows.Next() {
		need, err := scanNeed(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, need)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) Update(ctx context.Context, need Need, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := need.Validate(); err != nil {
		return err
	}
	minAmt, maxAmt, currency := budgetArgs(need.Budget)
	n, err := p.db.Exec(ctx, `
		UPDATE needs.needs SET
			title = $2,
			description = $3,
			category_id = $4,
			status = $5,
			budget_min_amount = $6,
			budget_max_amount = $7,
			budget_currency = $8,
			point = ST_SetSRID(ST_MakePoint($9, $10), 4326)::geography,
			radius_km = $11,
			updated_at = $12,
			expires_at = $13
		WHERE id = $1 AND requester_user_id = $14 AND updated_at = $15`,
		need.ID, need.Title, nullIfEmpty(need.Description), idArg(need.CategoryID),
		string(need.Status), minAmt, maxAmt, currency,
		need.Longitude, need.Latitude, need.RadiusKm,
		need.UpdatedAt, need.ExpiresAt, need.RequesterUserID, expectedUpdatedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		_, getErr := p.Get(ctx, need.ID)
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

func scanNeed(row interface {
	Scan(dest ...any) error
}) (Need, error) {
	var n Need
	var status string
	var description *string
	var category *ID
	var minAmt, maxAmt, currency *string
	if err := row.Scan(
		&n.ID, &n.RequesterUserID, &n.Title, &description, &category, &status,
		&minAmt, &maxAmt, &currency,
		&n.Latitude, &n.Longitude, &n.RadiusKm,
		&n.CreatedAt, &n.UpdatedAt, &n.ExpiresAt,
	); err != nil {
		return Need{}, err
	}
	if description != nil {
		n.Description = *description
	}
	n.CategoryID = cloneIDPtr(category)
	n.Status = Status(status)
	if minAmt != nil || maxAmt != nil || currency != nil {
		b := &Budget{}
		if minAmt != nil {
			b.MinAmount = *minAmt
		}
		if maxAmt != nil {
			b.MaxAmount = *maxAmt
		}
		if currency != nil {
			b.Currency = *currency
		}
		n.Budget = b
	}
	return n, nil
}

func budgetArgs(b *Budget) (minAmt, maxAmt, currency any) {
	if b == nil {
		return nil, nil, nil
	}
	return b.MinAmount, b.MaxAmount, b.Currency
}

func idArg(id *ID) any {
	if id == nil {
		return nil
	}
	return *id
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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
