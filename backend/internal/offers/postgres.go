package offers

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

const offerSelectCols = `id, need_id, provider_business_id, service_id, provider_user_id,
	message, price_amount::text, price_currency, status, created_at, updated_at`

func (p *PostgresStore) Create(ctx context.Context, offer Offer) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := offer.Validate(); err != nil {
		return err
	}
	amount, currency := priceArgs(offer.Price)
	_, err := p.db.Exec(ctx, `
		INSERT INTO offers.offers (
			id, need_id, provider_business_id, service_id, provider_user_id,
			message, price_amount, price_currency, status, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		offer.ID, offer.NeedID, offer.ProviderBusinessID, offer.ServiceID, offer.ProviderUserID,
		nullIfEmpty(offer.Message), amount, currency, string(offer.Status), offer.CreatedAt, offer.UpdatedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, id ID) (Offer, error) {
	if p == nil || p.db == nil {
		return Offer{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+offerSelectCols+`
		FROM offers.offers
		WHERE id = $1`, id)
	got, err := scanOffer(row)
	if err != nil {
		return Offer{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) ListByNeed(ctx context.Context, needID ID) ([]Offer, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+offerSelectCols+`
		FROM offers.offers
		WHERE need_id = $1
		ORDER BY created_at DESC, id ASC`, needID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	return scanOffers(rows)
}

func (p *PostgresStore) ListByProvider(ctx context.Context, providerUserID ID) ([]Offer, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+offerSelectCols+`
		FROM offers.offers
		WHERE provider_user_id = $1
		ORDER BY created_at DESC, id ASC`, providerUserID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	return scanOffers(rows)
}

func (p *PostgresStore) FindSubmitted(ctx context.Context, needID, serviceID ID) (Offer, error) {
	if p == nil || p.db == nil {
		return Offer{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+offerSelectCols+`
		FROM offers.offers
		WHERE need_id = $1 AND service_id = $2 AND status = 'submitted'`, needID, serviceID)
	got, err := scanOffer(row)
	if err != nil {
		return Offer{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) Update(ctx context.Context, offer Offer, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := offer.Validate(); err != nil {
		return err
	}
	amount, currency := priceArgs(offer.Price)
	n, err := p.db.Exec(ctx, `
		UPDATE offers.offers SET
			message = $2,
			price_amount = $3,
			price_currency = $4,
			status = $5,
			updated_at = $6
		WHERE id = $1 AND updated_at = $7`,
		offer.ID, nullIfEmpty(offer.Message), amount, currency, string(offer.Status), offer.UpdatedAt, expectedUpdatedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		_, getErr := p.Get(ctx, offer.ID)
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

func (p *PostgresStore) AcceptExclusive(ctx context.Context, offerID, needID ID, now time.Time) (Offer, error) {
	if p == nil || p.db == nil {
		return Offer{}, errUnavailable
	}
	if db.TxFrom(ctx) != nil {
		return p.acceptExclusive(ctx, offerID, needID, now)
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return Offer{}, mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	got, err := p.acceptExclusive(db.WithTx(ctx, tx), offerID, needID, now)
	if err != nil {
		return Offer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Offer{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) acceptExclusive(ctx context.Context, offerID, needID ID, now time.Time) (Offer, error) {
	rows, err := p.db.Query(ctx, `
		SELECT `+offerSelectCols+`
		FROM offers.offers
		WHERE need_id = $1
		ORDER BY id
		FOR UPDATE`, needID)
	if err != nil {
		return Offer{}, mapDBErr(err)
	}
	defer rows.Close()
	list, err := scanOffers(rows)
	if err != nil {
		return Offer{}, err
	}
	rows.Close()
	var current *Offer
	for i := range list {
		if list[i].ID == offerID {
			current = &list[i]
			break
		}
	}
	if current == nil {
		return Offer{}, errNotFound
	}
	if current.Status == StatusAccepted {
		return cloneOffer(*current), nil
	}
	for _, other := range list {
		if other.ID != offerID && other.Status == StatusAccepted {
			return Offer{}, errConflict
		}
	}
	next, err := current.Accept(now)
	if err != nil {
		return Offer{}, err
	}
	if err := p.Update(ctx, next, current.UpdatedAt); err != nil {
		return Offer{}, err
	}
	for _, other := range list {
		if other.ID == offerID || other.Status != StatusSubmitted {
			continue
		}
		rejected, err := other.Reject(now)
		if err != nil {
			return Offer{}, err
		}
		if err := p.Update(ctx, rejected, other.UpdatedAt); err != nil {
			return Offer{}, err
		}
	}
	return next, nil
}

func scanOffers(rows interface {
	Next() bool
	Err() error
	Scan(dest ...any) error
}) ([]Offer, error) {
	out := make([]Offer, 0)
	for rows.Next() {
		got, err := scanOffer(rows)
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

func scanOffer(row interface {
	Scan(dest ...any) error
}) (Offer, error) {
	var o Offer
	var message *string
	var amount, currency *string
	var status string
	if err := row.Scan(
		&o.ID, &o.NeedID, &o.ProviderBusinessID, &o.ServiceID, &o.ProviderUserID,
		&message, &amount, &currency, &status, &o.CreatedAt, &o.UpdatedAt,
	); err != nil {
		return Offer{}, err
	}
	if message != nil {
		o.Message = *message
	}
	o.Status = Status(status)
	if amount != nil || currency != nil {
		p := &Price{}
		if amount != nil {
			p.Amount = *amount
		}
		if currency != nil {
			p.Currency = *currency
		}
		o.Price = p
	}
	return o, nil
}

func priceArgs(p *Price) (amount, currency any) {
	if p == nil {
		return nil, nil
	}
	return p.Amount, p.Currency
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
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
