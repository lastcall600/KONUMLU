package savedsearch

import (
	"context"
	"errors"

	"backend/internal/platform/db"
)

var _ savedSearchStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

func (p *PostgresStore) Insert(ctx context.Context, row SavedSearch) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := row.Validate(); err != nil {
		return err
	}
	north, south, east, west := viewportCols(row.Filters.Viewport)
	_, err := p.db.Exec(ctx, `
		INSERT INTO saved_search.saved_searches (
			id, user_id, name, q, category_id, min_price, max_price, currency,
			north, south, east, west, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		row.ID, row.UserID, row.Name, row.Filters.Q, row.Filters.CategoryID,
		row.Filters.MinPrice, row.Filters.MaxPrice, row.Filters.Currency,
		north, south, east, west, row.CreatedAt.UTC(), row.UpdatedAt.UTC(),
	)
	return mapDBErr(err)
}

func (p *PostgresStore) GetByUser(ctx context.Context, userID, id ID) (SavedSearch, error) {
	if p == nil || p.db == nil {
		return SavedSearch{}, errUnavailable
	}
	if userID.IsZero() || id.IsZero() {
		return SavedSearch{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, user_id, name, q, category_id, min_price, max_price, currency,
			north, south, east, west, created_at, updated_at
		FROM saved_search.saved_searches
		WHERE id = $1 AND user_id = $2`, id, userID)
	return scanSaved(row)
}

func (p *PostgresStore) ListByUser(ctx context.Context, userID ID) ([]SavedSearch, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if userID.IsZero() {
		return nil, errZeroID
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, user_id, name, q, category_id, min_price, max_price, currency,
			north, south, east, west, created_at, updated_at
		FROM saved_search.saved_searches
		WHERE user_id = $1
		ORDER BY created_at DESC, id ASC`, userID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]SavedSearch, 0)
	for rows.Next() {
		row, err := scanSaved(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) DeleteByUser(ctx context.Context, userID, id ID) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if userID.IsZero() || id.IsZero() {
		return errZeroID
	}
	n, err := p.db.Exec(ctx, `
		DELETE FROM saved_search.saved_searches
		WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

func viewportCols(v *Viewport) (north, south, east, west any) {
	if v == nil {
		return nil, nil, nil, nil
	}
	return v.North, v.South, v.East, v.West
}

func scanSaved(row interface {
	Scan(dest ...any) error
}) (SavedSearch, error) {
	var (
		out            SavedSearch
		q              *string
		categoryID     *ID
		minPrice       *string
		maxPrice       *string
		currency       *string
		north, south   *float64
		east, west     *float64
	)
	if err := row.Scan(
		&out.ID, &out.UserID, &out.Name, &q, &categoryID, &minPrice, &maxPrice, &currency,
		&north, &south, &east, &west, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return SavedSearch{}, mapDBErr(err)
	}
	out.Filters = Filters{
		Q:          q,
		CategoryID: categoryID,
		MinPrice:   minPrice,
		MaxPrice:   maxPrice,
		Currency:   currency,
	}
	if north != nil || south != nil || east != nil || west != nil {
		if north == nil || south == nil || east == nil || west == nil {
			return SavedSearch{}, errUnavailable
		}
		out.Filters.Viewport = &Viewport{North: *north, South: *south, East: *east, West: *west}
	}
	return out, nil
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
