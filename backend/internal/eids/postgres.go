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

func (p *PostgresStore) BindSubject(ctx context.Context, b SubjectBinding) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if validateSubjectBinding(b) != nil {
		return errInvalidVerification
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO eids.subject_refs (subject_ref, verification_id, listing_id, verification_type, created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (verification_id) DO NOTHING`,
		b.SubjectRef, b.VerificationID, b.ListingID, string(b.VerificationType), b.CreatedAt)
	return mapDBErr(err)
}

func (p *PostgresStore) LookupSubject(ctx context.Context, subjectRef string) (SubjectBinding, error) {
	if p == nil || p.db == nil {
		return SubjectBinding{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT subject_ref, verification_id, listing_id, verification_type, created_at
		FROM eids.subject_refs WHERE subject_ref = $1
		FOR UPDATE`, subjectRef)
	return scanSubject(row)
}

func (p *PostgresStore) LookupSubjectByVerification(ctx context.Context, verificationID ID) (SubjectBinding, error) {
	if p == nil || p.db == nil {
		return SubjectBinding{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT subject_ref, verification_id, listing_id, verification_type, created_at
		FROM eids.subject_refs WHERE verification_id = $1`, verificationID)
	return scanSubject(row)
}

func (p *PostgresStore) GetReplay(ctx context.Context, decisionID string) (ReplayRecord, error) {
	if p == nil || p.db == nil {
		return ReplayRecord{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT decision_id, claims_hash, subject_ref, verification_type, status,
			issued_at, valid_until, key_id, received_at, applied_at
		FROM eids.tr_signed_decisions WHERE decision_id = $1`, decisionID)
	return scanReplay(row)
}

func (p *PostgresStore) LatestReplayIssuedAt(ctx context.Context, subjectRef string) (time.Time, bool, error) {
	if p == nil || p.db == nil {
		return time.Time{}, false, errUnavailable
	}
	var issued time.Time
	err := p.db.QueryRow(ctx, `
		SELECT issued_at FROM eids.tr_signed_decisions
		WHERE subject_ref = $1
		ORDER BY issued_at DESC
		LIMIT 1`, subjectRef).Scan(&issued)
	if err != nil {
		if errors.Is(mapDBErr(err), errNotFound) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, mapDBErr(err)
	}
	return issued, true, nil
}

func (p *PostgresStore) InsertReplay(ctx context.Context, rec ReplayRecord) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, insertReplaySQL,
		rec.DecisionID, rec.ClaimsHash[:], rec.SubjectRef, string(rec.VerificationType), rec.Status,
		rec.IssuedAt, rec.ValidUntil, rec.KeyID, rec.ReceivedAt, rec.AppliedAt)
	return mapDBErr(err)
}

func (p *PostgresStore) ApplyReplayAndUpdate(ctx context.Context, rec ReplayRecord, next Verification, expectedUpdatedAt time.Time) error {
	if err := p.InsertReplay(ctx, rec); err != nil {
		return err
	}
	return p.Update(ctx, next, expectedUpdatedAt)
}

func scanSubject(row scanner) (SubjectBinding, error) {
	var b SubjectBinding
	var kind string
	if err := row.Scan(&b.SubjectRef, &b.VerificationID, &b.ListingID, &kind, &b.CreatedAt); err != nil {
		return SubjectBinding{}, mapDBErr(err)
	}
	b.VerificationType = VerificationType(kind)
	return b, nil
}

func scanReplay(row scanner) (ReplayRecord, error) {
	var rec ReplayRecord
	var hash []byte
	var kind, status string
	if err := row.Scan(&rec.DecisionID, &hash, &rec.SubjectRef, &kind, &status,
		&rec.IssuedAt, &rec.ValidUntil, &rec.KeyID, &rec.ReceivedAt, &rec.AppliedAt); err != nil {
		return ReplayRecord{}, mapDBErr(err)
	}
	if len(hash) != 32 {
		return ReplayRecord{}, errUnavailable
	}
	copy(rec.ClaimsHash[:], hash)
	rec.VerificationType = VerificationType(kind)
	rec.Status = status
	return rec, nil
}

const insertReplaySQL = `
		INSERT INTO eids.tr_signed_decisions (
			decision_id, claims_hash, subject_ref, verification_type, status,
			issued_at, valid_until, key_id, received_at, applied_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`

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
