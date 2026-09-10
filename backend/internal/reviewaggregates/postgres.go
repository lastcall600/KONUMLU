package reviewaggregates

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/db"
)

var _ projectionStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

func (p *PostgresStore) ApplyVerified(ctx context.Context, event VerifiedReview, now time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return p.withTx(ctx, func(ctx context.Context) error {
		inserted, err := p.markProcessed(ctx, event.EventID, now)
		if err != nil {
			return err
		}
		if !inserted {
			return nil
		}
		if err := p.bumpListing(ctx, event.ListingID, event.ListingAccuracy, now); err != nil {
			return err
		}
		return p.bumpProvider(ctx, event.ProviderUserID, event.ProviderService, now)
	})
}

func (p *PostgresStore) markProcessed(ctx context.Context, eventID ID, now time.Time) (bool, error) {
	n, err := p.db.Exec(ctx, `
		INSERT INTO review_aggregates.processed_events (event_id, processed_at)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING`, eventID, now.UTC())
	if err != nil {
		return false, mapDBErr(err)
	}
	return n == 1, nil
}

func (p *PostgresStore) bumpListing(ctx context.Context, listingID ID, rating int, now time.Time) error {
	_, err := p.db.Exec(ctx, `
		INSERT INTO review_aggregates.listing_accuracy (
			listing_id, review_count, rating_sum, average_rating, updated_at
		) VALUES ($1, 1, $2, ($2::numeric / 1), $3)
		ON CONFLICT (listing_id) DO UPDATE SET
			review_count = review_aggregates.listing_accuracy.review_count + 1,
			rating_sum = review_aggregates.listing_accuracy.rating_sum + EXCLUDED.rating_sum,
			average_rating = (review_aggregates.listing_accuracy.rating_sum + EXCLUDED.rating_sum)::numeric
				/ (review_aggregates.listing_accuracy.review_count + 1),
			updated_at = EXCLUDED.updated_at`,
		listingID, rating, now.UTC())
	return mapDBErr(err)
}

func (p *PostgresStore) bumpProvider(ctx context.Context, providerUserID ID, rating int, now time.Time) error {
	_, err := p.db.Exec(ctx, `
		INSERT INTO review_aggregates.provider_service (
			provider_user_id, review_count, rating_sum, average_rating, updated_at
		) VALUES ($1, 1, $2, ($2::numeric / 1), $3)
		ON CONFLICT (provider_user_id) DO UPDATE SET
			review_count = review_aggregates.provider_service.review_count + 1,
			rating_sum = review_aggregates.provider_service.rating_sum + EXCLUDED.rating_sum,
			average_rating = (review_aggregates.provider_service.rating_sum + EXCLUDED.rating_sum)::numeric
				/ (review_aggregates.provider_service.review_count + 1),
			updated_at = EXCLUDED.updated_at`,
		providerUserID, rating, now.UTC())
	return mapDBErr(err)
}

func (p *PostgresStore) GetListingAccuracy(ctx context.Context, listingID ID) (RatingSummary, error) {
	if p == nil || p.db == nil {
		return RatingSummary{}, errUnavailable
	}
	if listingID.IsZero() {
		return RatingSummary{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT review_count, rating_sum, updated_at
		FROM review_aggregates.listing_accuracy
		WHERE listing_id = $1`, listingID)
	got, err := scanSummary(row)
	if err != nil {
		return RatingSummary{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) GetProviderService(ctx context.Context, providerUserID ID) (RatingSummary, error) {
	if p == nil || p.db == nil {
		return RatingSummary{}, errUnavailable
	}
	if providerUserID.IsZero() {
		return RatingSummary{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT review_count, rating_sum, updated_at
		FROM review_aggregates.provider_service
		WHERE provider_user_id = $1`, providerUserID)
	got, err := scanSummary(row)
	if err != nil {
		return RatingSummary{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) withTx(ctx context.Context, fn func(context.Context) error) error {
	if db.TxFrom(ctx) != nil {
		return fn(ctx)
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(db.WithTx(ctx, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBErr(err)
	}
	return nil
}

func scanSummary(row db.Row) (RatingSummary, error) {
	var s RatingSummary
	if err := row.Scan(&s.ReviewCount, &s.RatingSum, &s.UpdatedAt); err != nil {
		return RatingSummary{}, err
	}
	return s, nil
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, db.ErrUnavailable) {
		return errUnavailable
	}
	if errors.Is(err, errZeroID) || errors.Is(err, errInvalidEvent) ||
		errors.Is(err, errInvalidRating) || errors.Is(err, errInvalidSummary) ||
		errors.Is(err, errStoreRequired) || errors.Is(err, errNotFound) {
		return err
	}
	return errUnavailable
}
