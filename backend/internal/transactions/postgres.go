package transactions

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

const txnSelectCols = `id, offer_id, need_id, requester_user_id, provider_user_id,
	provider_business_id, service_id, agreed_amount::text, agreed_currency, status,
	created_at, updated_at, completed_at, cancelled_at`

func (p *PostgresStore) Create(ctx context.Context, txn Transaction) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := txn.Validate(); err != nil {
		return err
	}
	amount, currency := priceArgs(txn.Price)
	_, err := p.db.Exec(ctx, `
		INSERT INTO transactions.transactions (
			id, offer_id, need_id, requester_user_id, provider_user_id,
			provider_business_id, service_id, agreed_amount, agreed_currency, status,
			created_at, updated_at, completed_at, cancelled_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		txn.ID, txn.OfferID, txn.NeedID, txn.RequesterUserID, txn.ProviderUserID,
		txn.ProviderBusinessID, txn.ServiceID, amount, currency, string(txn.Status),
		txn.CreatedAt, txn.UpdatedAt, txn.CompletedAt, txn.CancelledAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, id ID) (Transaction, error) {
	if p == nil || p.db == nil {
		return Transaction{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+txnSelectCols+`
		FROM transactions.transactions
		WHERE id = $1`, id)
	got, err := scanTxn(row)
	if err != nil {
		return Transaction{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) GetByOfferID(ctx context.Context, offerID ID) (Transaction, error) {
	if p == nil || p.db == nil {
		return Transaction{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+txnSelectCols+`
		FROM transactions.transactions
		WHERE offer_id = $1`, offerID)
	got, err := scanTxn(row)
	if err != nil {
		return Transaction{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) ListForParticipant(ctx context.Context, userID ID) ([]Transaction, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+txnSelectCols+`
		FROM transactions.transactions
		WHERE requester_user_id = $1 OR provider_user_id = $1
		ORDER BY created_at DESC, id ASC`, userID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Transaction, 0)
	for rows.Next() {
		got, err := scanTxn(rows)
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

func (p *PostgresStore) Update(ctx context.Context, txn Transaction, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := txn.Validate(); err != nil {
		return err
	}
	amount, currency := priceArgs(txn.Price)
	n, err := p.db.Exec(ctx, `
		UPDATE transactions.transactions SET
			agreed_amount = $2,
			agreed_currency = $3,
			status = $4,
			updated_at = $5,
			completed_at = $6,
			cancelled_at = $7
		WHERE id = $1 AND updated_at = $8`,
		txn.ID, amount, currency, string(txn.Status), txn.UpdatedAt, txn.CompletedAt, txn.CancelledAt, expectedUpdatedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		_, getErr := p.Get(ctx, txn.ID)
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

func scanTxn(row interface {
	Scan(dest ...any) error
}) (Transaction, error) {
	var t Transaction
	var amount, currency *string
	var status string
	if err := row.Scan(
		&t.ID, &t.OfferID, &t.NeedID, &t.RequesterUserID, &t.ProviderUserID,
		&t.ProviderBusinessID, &t.ServiceID, &amount, &currency, &status,
		&t.CreatedAt, &t.UpdatedAt, &t.CompletedAt, &t.CancelledAt,
	); err != nil {
		return Transaction{}, err
	}
	t.Status = Status(status)
	if amount != nil || currency != nil {
		p := &Price{}
		if amount != nil {
			p.Amount = *amount
		}
		if currency != nil {
			p.Currency = *currency
		}
		t.Price = p
	}
	return t, nil
}

func priceArgs(p *Price) (amount, currency any) {
	if p == nil {
		return nil, nil
	}
	return p.Amount, p.Currency
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
