package favorites

import (
	"context"
	"errors"

	"backend/internal/platform/db"
)

var _ favoriteStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

func (p *PostgresStore) Add(ctx context.Context, fav Favorite) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := fav.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO favorites.listing_favorites (user_id, listing_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, listing_id) DO NOTHING`,
		fav.UserID, fav.ListingID, fav.CreatedAt.UTC(),
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Remove(ctx context.Context, userID, listingID ID) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, `
		DELETE FROM favorites.listing_favorites
		WHERE user_id = $1 AND listing_id = $2`, userID, listingID)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, userID, listingID ID) (Favorite, error) {
	if p == nil || p.db == nil {
		return Favorite{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT user_id, listing_id, created_at
		FROM favorites.listing_favorites
		WHERE user_id = $1 AND listing_id = $2`, userID, listingID)
	return scanFavorite(row)
}

func (p *PostgresStore) ListByUser(ctx context.Context, userID ID) ([]Favorite, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT user_id, listing_id, created_at
		FROM favorites.listing_favorites
		WHERE user_id = $1
		ORDER BY created_at DESC, listing_id ASC`, userID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Favorite, 0)
	for rows.Next() {
		fav, err := scanFavorite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fav)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func scanFavorite(row interface {
	Scan(dest ...any) error
}) (Favorite, error) {
	var fav Favorite
	if err := row.Scan(&fav.UserID, &fav.ListingID, &fav.CreatedAt); err != nil {
		return Favorite{}, mapDBErr(err)
	}
	return fav, nil
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
