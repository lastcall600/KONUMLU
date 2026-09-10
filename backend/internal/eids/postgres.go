package eids

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

const verificationSelectCols = `id, listing_id, verification_type, status, provider_reference, failure_code,
	created_at, updated_at, requested_at, verified_at, failed_at, expires_at`

func (p *PostgresStore) Create(ctx context.Context, v Verification) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO eids.verifications (
			id, listing_id, verification_type, status, provider_reference, failure_code,
			created_at, updated_at, requested_at, verified_at, failed_at, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		v.ID, v.ListingID, string(v.VerificationType), string(v.Status), v.ProviderReference, nullFailure(v.FailureCode),
		v.CreatedAt, v.UpdatedAt, v.RequestedAt, v.VerifiedAt, v.FailedAt, v.ExpiresAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, id ID) (Verification, error) {
	if p == nil || p.db == nil {
		return Verification{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+verificationSelectCols+`
		FROM eids.verifications
		WHERE id = $1`, id)
	got, err := scanVerification(row)
	if err != nil {
		return Verification{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) GetOpen(ctx context.Context, listingID ID, kind VerificationType) (Verification, error) {
	if p == nil || p.db == nil {
		return Verification{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+verificationSelectCols+`
		FROM eids.verifications
		WHERE listing_id = $1 AND verification_type = $2
			AND status IN ('pending', 'in_progress', 'unavailable')
		ORDER BY updated_at DESC, id DESC
		LIMIT 1`, listingID, string(kind))
	got, err := scanVerification(row)
	if err != nil {
		return Verification{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) GetLatest(ctx context.Context, listingID ID, kind VerificationType) (Verification, error) {
	if p == nil || p.db == nil {
		return Verification{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+verificationSelectCols+`
		FROM eids.verifications
		WHERE listing_id = $1 AND verification_type = $2
		ORDER BY updated_at DESC, id DESC
		LIMIT 1`, listingID, string(kind))
	got, err := scanVerification(row)
	if err != nil {
		return Verification{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) Update(ctx context.Context, v Verification, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := v.Validate(); err != nil {
		return err
	}
	n, err := p.db.Exec(ctx, `
		UPDATE eids.verifications SET
			status = $2,
			provider_reference = $3,
			failure_code = $4,
			updated_at = $5,
			requested_at = $6,
			verified_at = $7,
			failed_at = $8,
			expires_at = $9
		WHERE id = $1 AND updated_at = $10`,
		v.ID, string(v.Status), v.ProviderReference, nullFailure(v.FailureCode),
		v.UpdatedAt, v.RequestedAt, v.VerifiedAt, v.FailedAt, v.ExpiresAt, expectedUpdatedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errConflict
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanVerification(row scanner) (Verification, error) {
	var v Verification
	var kind, status string
	var failure *string
	if err := row.Scan(
		&v.ID, &v.ListingID, &kind, &status, &v.ProviderReference, &failure,
		&v.CreatedAt, &v.UpdatedAt, &v.RequestedAt, &v.VerifiedAt, &v.FailedAt, &v.ExpiresAt,
	); err != nil {
		return Verification{}, err
	}
	v.VerificationType = VerificationType(kind)
	v.Status = Status(status)
	if failure != nil {
		v.FailureCode = FailureCode(*failure)
	}
	return v, nil
}

func nullFailure(c FailureCode) *string {
	if c == "" {
		return nil
	}
	s := string(c)
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
