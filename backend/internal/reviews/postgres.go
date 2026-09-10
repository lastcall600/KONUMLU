package reviews

import (
	"context"
	"errors"

	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
)

var _ reviewStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

const reviewSelectCols = `r.id, r.verified_interaction_id, r.listing_id, r.reviewer_user_id, r.provider_user_id,
	r.body, r.created_at, r.updated_at, a.overall, s.overall`

func (p *PostgresStore) InsertReview(ctx context.Context, row Review, enqueue func(ctx context.Context, exec outbox.Execer) error) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := row.Validate(); err != nil {
		return err
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO reviews.reviews (
			id, verified_interaction_id, listing_id, reviewer_user_id, provider_user_id,
			body, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		row.ID, row.VerifiedInteractionID, row.ListingID, row.ReviewerUserID, row.ProviderUserID,
		row.Body, row.CreatedAt.UTC(), row.UpdatedAt.UTC(),
	)
	if err != nil {
		if errors.Is(err, db.ErrConflict) {
			return errConflict
		}
		return mapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO reviews.listing_accuracy_ratings (review_id, overall)
		VALUES ($1, $2)`, row.ID, row.ListingAccuracy); err != nil {
		return mapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO reviews.provider_service_ratings (review_id, overall)
		VALUES ($1, $2)`, row.ID, row.ProviderService); err != nil {
		return mapDBErr(err)
	}
	if enqueue != nil {
		if err := enqueue(ctx, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBErr(err)
	}
	return nil
}

func (p *PostgresStore) GetByInteraction(ctx context.Context, verifiedInteractionID ID) (Review, error) {
	if p == nil || p.db == nil {
		return Review{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+reviewSelectCols+`
		FROM reviews.reviews r
		JOIN reviews.listing_accuracy_ratings a ON a.review_id = r.id
		JOIN reviews.provider_service_ratings s ON s.review_id = r.id
		WHERE r.verified_interaction_id = $1`, verifiedInteractionID)
	return scanReview(row)
}

func (p *PostgresStore) ListForReviewer(ctx context.Context, reviewerUserID ID, limit int) ([]Review, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if limit <= 0 {
		limit = MaxReviews
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+reviewSelectCols+`
		FROM reviews.reviews r
		JOIN reviews.listing_accuracy_ratings a ON a.review_id = r.id
		JOIN reviews.provider_service_ratings s ON s.review_id = r.id
		WHERE r.reviewer_user_id = $1
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $2`, reviewerUserID, limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Review, 0)
	for rows.Next() {
		row, err := scanReview(rows)
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

func (p *PostgresStore) ListForListing(ctx context.Context, listingID ID, cursor *listingCursor, limit int) ([]Review, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if listingID.IsZero() {
		return nil, errZeroID
	}
	if limit <= 0 {
		limit = MaxPublicReviews
	}
	var rows db.Rows
	var err error
	if cursor == nil {
		rows, err = p.db.Query(ctx, `
		SELECT `+reviewSelectCols+`
		FROM reviews.reviews r
		JOIN reviews.listing_accuracy_ratings a ON a.review_id = r.id
		JOIN reviews.provider_service_ratings s ON s.review_id = r.id
		WHERE r.listing_id = $1
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $2`, listingID, limit)
	} else {
		rows, err = p.db.Query(ctx, `
		SELECT `+reviewSelectCols+`
		FROM reviews.reviews r
		JOIN reviews.listing_accuracy_ratings a ON a.review_id = r.id
		JOIN reviews.provider_service_ratings s ON s.review_id = r.id
		WHERE r.listing_id = $1
		AND (r.created_at < $2 OR (r.created_at = $2 AND r.id < $3))
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $4`, listingID, cursor.CreatedAt.UTC(), cursor.ID, limit)
	}
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Review, 0)
	for rows.Next() {
		row, err := scanReview(rows)
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

func scanReview(row interface {
	Scan(dest ...any) error
}) (Review, error) {
	var r Review
	if err := row.Scan(
		&r.ID, &r.VerifiedInteractionID, &r.ListingID, &r.ReviewerUserID, &r.ProviderUserID,
		&r.Body, &r.CreatedAt, &r.UpdatedAt, &r.ListingAccuracy, &r.ProviderService,
	); err != nil {
		return Review{}, mapDBErr(err)
	}
	return r, nil
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
