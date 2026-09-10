package payments

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

const paymentSelectCols = `id, transaction_id, payer_user_id, payee_user_id,
	amount::text, currency, status, provider, provider_reference,
	created_at, updated_at, authorized_at, captured_at, cancelled_at, failed_at`

func (p *PostgresStore) Create(ctx context.Context, pay Payment) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := pay.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO payments.payments (
			id, transaction_id, payer_user_id, payee_user_id,
			amount, currency, status, provider, provider_reference,
			created_at, updated_at, authorized_at, captured_at, cancelled_at, failed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		pay.ID, pay.TransactionID, pay.PayerUserID, pay.PayeeUserID,
		pay.Amount.Amount, pay.Amount.Currency, string(pay.Status), pay.Provider, pay.ProviderReference,
		pay.CreatedAt, pay.UpdatedAt, pay.AuthorizedAt, pay.CapturedAt, pay.CancelledAt, pay.FailedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, id ID) (Payment, error) {
	if p == nil || p.db == nil {
		return Payment{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+paymentSelectCols+`
		FROM payments.payments
		WHERE id = $1`, id)
	got, err := scanPayment(row)
	if err != nil {
		return Payment{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) GetByTransactionID(ctx context.Context, transactionID ID) (Payment, error) {
	if p == nil || p.db == nil {
		return Payment{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+paymentSelectCols+`
		FROM payments.payments
		WHERE transaction_id = $1`, transactionID)
	got, err := scanPayment(row)
	if err != nil {
		return Payment{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) ListForParticipant(ctx context.Context, userID ID) ([]Payment, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+paymentSelectCols+`
		FROM payments.payments
		WHERE payer_user_id = $1 OR payee_user_id = $1
		ORDER BY created_at DESC, id ASC`, userID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Payment, 0)
	for rows.Next() {
		got, err := scanPayment(rows)
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

func (p *PostgresStore) Update(ctx context.Context, pay Payment, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := pay.Validate(); err != nil {
		return err
	}
	n, err := p.db.Exec(ctx, `
		UPDATE payments.payments SET
			status = $2,
			provider = $3,
			provider_reference = $4,
			updated_at = $5,
			authorized_at = $6,
			captured_at = $7,
			cancelled_at = $8,
			failed_at = $9
		WHERE id = $1 AND updated_at = $10`,
		pay.ID, string(pay.Status), pay.Provider, pay.ProviderReference, pay.UpdatedAt,
		pay.AuthorizedAt, pay.CapturedAt, pay.CancelledAt, pay.FailedAt, expectedUpdatedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		_, getErr := p.Get(ctx, pay.ID)
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

func scanPayment(row interface {
	Scan(dest ...any) error
}) (Payment, error) {
	var p Payment
	var status string
	if err := row.Scan(
		&p.ID, &p.TransactionID, &p.PayerUserID, &p.PayeeUserID,
		&p.Amount.Amount, &p.Amount.Currency, &status, &p.Provider, &p.ProviderReference,
		&p.CreatedAt, &p.UpdatedAt, &p.AuthorizedAt, &p.CapturedAt, &p.CancelledAt, &p.FailedAt,
	); err != nil {
		return Payment{}, err
	}
	p.Status = Status(status)
	return p, nil
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
