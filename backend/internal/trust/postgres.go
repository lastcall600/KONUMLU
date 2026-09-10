package trust

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

func (p *PostgresStore) ApplyCompleted(ctx context.Context, event CompletedInteraction, policy LevelPolicy, now time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if err := policy.Validate(); err != nil {
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
		if err := p.insertHistory(ctx, HistoryEntry{
			InteractionID:      event.InteractionID,
			UserID:             event.RequesterUserID,
			Role:               RoleRequester,
			ListingID:          event.ListingID,
			InteractionType:    event.InteractionType,
			VerificationMethod: event.VerificationMethod,
			VerifiedAt:         event.VerifiedAt.UTC(),
		}, policy, now); err != nil {
			return err
		}
		return p.insertHistory(ctx, HistoryEntry{
			InteractionID:      event.InteractionID,
			UserID:             event.ProviderUserID,
			Role:               RoleProvider,
			ListingID:          event.ListingID,
			InteractionType:    event.InteractionType,
			VerificationMethod: event.VerificationMethod,
			VerifiedAt:         event.VerifiedAt.UTC(),
		}, policy, now)
	})
}

func (p *PostgresStore) ApplyVerifiedReview(ctx context.Context, event VerifiedReview, now time.Time) error {
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
		if err := p.bumpReviewer(ctx, event.ReviewerUserID, event.CreatedAt, now); err != nil {
			return err
		}
		return p.bumpProviderService(ctx, event.ProviderUserID, event.ProviderService, event.CreatedAt, now)
	})
}

func (p *PostgresStore) markProcessed(ctx context.Context, eventID ID, now time.Time) (bool, error) {
	n, err := p.db.Exec(ctx, `
		INSERT INTO trust.processed_events (event_id, processed_at)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING`, eventID, now.UTC())
	if err != nil {
		return false, mapDBErr(err)
	}
	return n == 1, nil
}

func (p *PostgresStore) insertHistory(ctx context.Context, row HistoryEntry, policy LevelPolicy, now time.Time) error {
	if err := row.Validate(); err != nil {
		return err
	}
	n, err := p.db.Exec(ctx, `
		INSERT INTO trust.user_verified_history (
			interaction_id, user_id, role, listing_id, interaction_type, verification_method, verified_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (interaction_id, user_id) DO NOTHING`,
		row.InteractionID, row.UserID, row.Role, row.ListingID,
		row.InteractionType, row.VerificationMethod, row.VerifiedAt.UTC(),
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return nil
	}
	if !countsTowardTrustLevel(row.InteractionType) {
		return nil
	}
	return p.bumpProfile(ctx, row.UserID, row.Role, row.VerifiedAt, policy, now)
}

func (p *PostgresStore) bumpProfile(ctx context.Context, userID ID, role string, verifiedAt time.Time, policy LevelPolicy, now time.Time) error {
	reqInc, provInc := 0, 0
	switch role {
	case RoleRequester:
		reqInc = 1
	case RoleProvider:
		provInc = 1
	default:
		return errInvalidEvent
	}
	row := p.db.QueryRow(ctx, `
		INSERT INTO trust.user_profiles (
			user_id, verified_interaction_count, requester_verified_interaction_count,
			provider_verified_interaction_count, last_verified_interaction_at, trust_level, updated_at,
			verified_review_count, provider_service_review_count, provider_service_rating_sum,
			provider_service_average, last_verified_review_at, last_provider_service_review_at
		) VALUES ($1, 1, $2, $3, $4, $5, $6, 0, 0, 0, NULL, NULL, NULL)
		ON CONFLICT (user_id) DO UPDATE SET
			verified_interaction_count = trust.user_profiles.verified_interaction_count + 1,
			requester_verified_interaction_count = trust.user_profiles.requester_verified_interaction_count + EXCLUDED.requester_verified_interaction_count,
			provider_verified_interaction_count = trust.user_profiles.provider_verified_interaction_count + EXCLUDED.provider_verified_interaction_count,
			last_verified_interaction_at = CASE
				WHEN trust.user_profiles.last_verified_interaction_at IS NULL THEN EXCLUDED.last_verified_interaction_at
				WHEN EXCLUDED.last_verified_interaction_at > trust.user_profiles.last_verified_interaction_at THEN EXCLUDED.last_verified_interaction_at
				ELSE trust.user_profiles.last_verified_interaction_at
			END,
			updated_at = EXCLUDED.updated_at
		RETURNING verified_interaction_count`,
		userID, reqInc, provInc, verifiedAt.UTC(), LevelNew, now.UTC(),
	)
	var count int
	if err := row.Scan(&count); err != nil {
		return mapDBErr(err)
	}
	level := policy.Level(count)
	_, err := p.db.Exec(ctx, `UPDATE trust.user_profiles SET trust_level = $2 WHERE user_id = $1`, userID, level)
	return mapDBErr(err)
}

func (p *PostgresStore) bumpReviewer(ctx context.Context, userID ID, createdAt, now time.Time) error {
	_, err := p.db.Exec(ctx, `
		INSERT INTO trust.user_profiles (
			user_id, verified_interaction_count, requester_verified_interaction_count,
			provider_verified_interaction_count, last_verified_interaction_at, trust_level, updated_at,
			verified_review_count, provider_service_review_count, provider_service_rating_sum,
			provider_service_average, last_verified_review_at, last_provider_service_review_at
		) VALUES ($1, 0, 0, 0, NULL, $2, $3, 1, 0, 0, NULL, $4, NULL)
		ON CONFLICT (user_id) DO UPDATE SET
			verified_review_count = trust.user_profiles.verified_review_count + 1,
			last_verified_review_at = CASE
				WHEN trust.user_profiles.last_verified_review_at IS NULL THEN EXCLUDED.last_verified_review_at
				WHEN EXCLUDED.last_verified_review_at > trust.user_profiles.last_verified_review_at THEN EXCLUDED.last_verified_review_at
				ELSE trust.user_profiles.last_verified_review_at
			END,
			updated_at = EXCLUDED.updated_at`,
		userID, LevelNew, now.UTC(), createdAt.UTC())
	return mapDBErr(err)
}

func (p *PostgresStore) bumpProviderService(ctx context.Context, userID ID, rating int, createdAt, now time.Time) error {
	_, err := p.db.Exec(ctx, `
		INSERT INTO trust.user_profiles (
			user_id, verified_interaction_count, requester_verified_interaction_count,
			provider_verified_interaction_count, last_verified_interaction_at, trust_level, updated_at,
			verified_review_count, provider_service_review_count, provider_service_rating_sum,
			provider_service_average, last_verified_review_at, last_provider_service_review_at
		) VALUES ($1, 0, 0, 0, NULL, $2, $3, 0, 1, $4, ($4::numeric / 1), NULL, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			provider_service_review_count = trust.user_profiles.provider_service_review_count + 1,
			provider_service_rating_sum = trust.user_profiles.provider_service_rating_sum + EXCLUDED.provider_service_rating_sum,
			provider_service_average = (trust.user_profiles.provider_service_rating_sum + EXCLUDED.provider_service_rating_sum)::numeric
				/ (trust.user_profiles.provider_service_review_count + 1),
			last_provider_service_review_at = CASE
				WHEN trust.user_profiles.last_provider_service_review_at IS NULL THEN EXCLUDED.last_provider_service_review_at
				WHEN EXCLUDED.last_provider_service_review_at > trust.user_profiles.last_provider_service_review_at THEN EXCLUDED.last_provider_service_review_at
				ELSE trust.user_profiles.last_provider_service_review_at
			END,
			updated_at = EXCLUDED.updated_at`,
		userID, LevelNew, now.UTC(), rating, createdAt.UTC())
	return mapDBErr(err)
}

func (p *PostgresStore) GetProfile(ctx context.Context, userID ID) (UserProfile, error) {
	if p == nil || p.db == nil {
		return UserProfile{}, errUnavailable
	}
	if userID.IsZero() {
		return UserProfile{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT user_id, verified_interaction_count, requester_verified_interaction_count,
			provider_verified_interaction_count, last_verified_interaction_at, trust_level, updated_at,
			verified_review_count, provider_service_review_count, provider_service_rating_sum,
			last_verified_review_at, last_provider_service_review_at
		FROM trust.user_profiles
		WHERE user_id = $1`, userID)
	got, err := scanProfile(row)
	if err != nil {
		return UserProfile{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) ListHistory(ctx context.Context, userID ID) ([]HistoryEntry, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if userID.IsZero() {
		return nil, errZeroID
	}
	rows, err := p.db.Query(ctx, `
		SELECT interaction_id, user_id, role, listing_id, interaction_type, verification_method, verified_at
		FROM trust.user_verified_history
		WHERE user_id = $1
		ORDER BY verified_at DESC, interaction_id DESC`, userID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]HistoryEntry, 0)
	for rows.Next() {
		row, err := scanHistory(rows)
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

func scanProfile(row db.Row) (UserProfile, error) {
	var p UserProfile
	if err := row.Scan(
		&p.UserID,
		&p.VerifiedInteractionCount,
		&p.RequesterVerifiedInteractionCount,
		&p.ProviderVerifiedInteractionCount,
		&p.LastVerifiedInteractionAt,
		&p.TrustLevel,
		&p.UpdatedAt,
		&p.VerifiedReviewCount,
		&p.ProviderServiceReviewCount,
		&p.ProviderServiceRatingSum,
		&p.LastVerifiedReviewAt,
		&p.LastProviderServiceReviewAt,
	); err != nil {
		return UserProfile{}, err
	}
	return p, nil
}

func scanHistory(row db.Row) (HistoryEntry, error) {
	var h HistoryEntry
	if err := row.Scan(
		&h.InteractionID, &h.UserID, &h.Role, &h.ListingID,
		&h.InteractionType, &h.VerificationMethod, &h.VerifiedAt,
	); err != nil {
		return HistoryEntry{}, err
	}
	return h, nil
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
		errors.Is(err, errInvalidPolicy) || errors.Is(err, errInvalidProfile) ||
		errors.Is(err, errStoreRequired) || errors.Is(err, errNotFound) {
		return err
	}
	return errUnavailable
}
