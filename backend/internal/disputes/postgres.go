package disputes

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

const disputeSelectCols = `id, transaction_id, requester_user_id, provider_user_id, opened_by_user_id,
	reason_code, statement, status, resolution_code, created_at, updated_at, resolved_at`

const evidenceSelectCols = `id, dispute_id, evidence_type, title, description, reference_value, actor_user_id, actor_role, created_at`

func (p *PostgresStore) Create(ctx context.Context, d Dispute) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := d.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO disputes.disputes (
			id, transaction_id, requester_user_id, provider_user_id, opened_by_user_id,
			reason_code, statement, status, resolution_code, created_at, updated_at, resolved_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		d.ID, d.TransactionID, d.RequesterUserID, d.ProviderUserID, d.OpenedByUserID,
		string(d.ReasonCode), d.Statement, string(d.Status), resolutionString(d.ResolutionCode),
		d.CreatedAt, d.UpdatedAt, d.ResolvedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, id ID) (Dispute, error) {
	if p == nil || p.db == nil {
		return Dispute{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+disputeSelectCols+`
		FROM disputes.disputes
		WHERE id = $1`, id)
	got, err := scanDispute(row)
	if err != nil {
		return Dispute{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) GetActiveByTransactionID(ctx context.Context, transactionID ID) (Dispute, error) {
	if p == nil || p.db == nil {
		return Dispute{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+disputeSelectCols+`
		FROM disputes.disputes
		WHERE transaction_id = $1 AND status IN ('open', 'under_review')
		LIMIT 1`, transactionID)
	got, err := scanDispute(row)
	if err != nil {
		return Dispute{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) ListConcludedByTransactionID(ctx context.Context, transactionID ID) ([]Dispute, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+disputeSelectCols+`
		FROM disputes.disputes
		WHERE transaction_id = $1 AND status IN ('resolved', 'closed')
		ORDER BY created_at DESC, id ASC`, transactionID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	return scanDisputes(rows)
}

func (p *PostgresStore) ListForParticipant(ctx context.Context, userID ID) ([]Dispute, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+disputeSelectCols+`
		FROM disputes.disputes
		WHERE requester_user_id = $1 OR provider_user_id = $1
		ORDER BY created_at DESC, id ASC`, userID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	return scanDisputes(rows)
}

func (p *PostgresStore) Update(ctx context.Context, d Dispute, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := d.Validate(); err != nil {
		return err
	}
	n, err := p.db.Exec(ctx, `
		UPDATE disputes.disputes SET
			status = $2,
			resolution_code = $3,
			updated_at = $4,
			resolved_at = $5
		WHERE id = $1 AND updated_at = $6`,
		d.ID, string(d.Status), resolutionString(d.ResolutionCode), d.UpdatedAt, d.ResolvedAt, expectedUpdatedAt,
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

func (p *PostgresStore) AppendEvidence(ctx context.Context, e Evidence) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := e.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO disputes.evidence (
			id, dispute_id, evidence_type, title, description, reference_value, actor_user_id, actor_role, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		e.ID, e.DisputeID, string(e.EvidenceType), e.Title, e.Description, e.ReferenceValue, e.ActorUserID, e.ActorRole, e.CreatedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) ListEvidence(ctx context.Context, disputeID ID) ([]Evidence, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+evidenceSelectCols+`
		FROM disputes.evidence
		WHERE dispute_id = $1
		ORDER BY created_at ASC, id ASC`, disputeID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Evidence, 0)
	for rows.Next() {
		got, err := scanEvidence(rows)
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

func scanDisputes(rows interface {
	Next() bool
	Err() error
	Scan(dest ...any) error
}) ([]Dispute, error) {
	out := make([]Dispute, 0)
	for rows.Next() {
		got, err := scanDispute(rows)
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

func scanDispute(row interface {
	Scan(dest ...any) error
}) (Dispute, error) {
	var d Dispute
	var reason, status string
	var resolution *string
	if err := row.Scan(
		&d.ID, &d.TransactionID, &d.RequesterUserID, &d.ProviderUserID, &d.OpenedByUserID,
		&reason, &d.Statement, &status, &resolution, &d.CreatedAt, &d.UpdatedAt, &d.ResolvedAt,
	); err != nil {
		return Dispute{}, err
	}
	d.ReasonCode = ReasonCode(reason)
	d.Status = Status(status)
	if resolution != nil {
		code := ResolutionCode(*resolution)
		d.ResolutionCode = &code
	}
	return d, nil
}

func scanEvidence(row interface {
	Scan(dest ...any) error
}) (Evidence, error) {
	var e Evidence
	var typ string
	if err := row.Scan(
		&e.ID, &e.DisputeID, &typ, &e.Title, &e.Description, &e.ReferenceValue, &e.ActorUserID, &e.ActorRole, &e.CreatedAt,
	); err != nil {
		return Evidence{}, err
	}
	e.EvidenceType = EvidenceType(typ)
	return e, nil
}

func resolutionString(c *ResolutionCode) *string {
	if c == nil {
		return nil
	}
	v := string(*c)
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
