package businesses

import (
	"context"
	"errors"
	"time"
)

const offeredServiceSelectCols = `id, business_id, title, description, status, price_model, price_amount::text, price_currency, category_id, created_at, updated_at`

func (p *PostgresStore) CreateOfferedService(ctx context.Context, svc OfferedService) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := svc.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO businesses.services (
			id, business_id, title, description, status, price_model, price_amount, price_currency, category_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		svc.ID, svc.BusinessID, svc.Title, nullIfEmpty(svc.Description), string(svc.Status),
		nullIfEmpty(string(svc.Price.Model)), svc.Price.Amount, svc.Price.Currency, idArg(svc.CategoryID), svc.CreatedAt, svc.UpdatedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) GetOfferedService(ctx context.Context, id ID) (OfferedService, error) {
	if p == nil || p.db == nil {
		return OfferedService{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+offeredServiceSelectCols+`
		FROM businesses.services
		WHERE id = $1`, id)
	got, err := scanOfferedService(row)
	if err != nil {
		return OfferedService{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) ListOfferedServices(ctx context.Context, businessID ID) ([]OfferedService, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+offeredServiceSelectCols+`
		FROM businesses.services
		WHERE business_id = $1
		ORDER BY created_at ASC, id ASC`, businessID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]OfferedService, 0)
	for rows.Next() {
		svc, err := scanOfferedService(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, svc)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) UpdateOfferedService(ctx context.Context, svc OfferedService, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := svc.Validate(); err != nil {
		return err
	}
	n, err := p.db.Exec(ctx, `
		UPDATE businesses.services SET
			title = $2,
			description = $3,
			status = $4,
			price_model = $5,
			price_amount = $6,
			price_currency = $7,
			category_id = $8,
			updated_at = $9
		WHERE id = $1 AND business_id = $10 AND updated_at = $11`,
		svc.ID, svc.Title, nullIfEmpty(svc.Description), string(svc.Status),
		nullIfEmpty(string(svc.Price.Model)), svc.Price.Amount, svc.Price.Currency, idArg(svc.CategoryID),
		svc.UpdatedAt, svc.BusinessID, expectedUpdatedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		_, getErr := p.GetOfferedService(ctx, svc.ID)
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

func scanOfferedService(row interface {
	Scan(dest ...any) error
}) (OfferedService, error) {
	var s OfferedService
	var status string
	var description *string
	var model *string
	var category *ID
	if err := row.Scan(
		&s.ID, &s.BusinessID, &s.Title, &description, &status, &model, &s.Price.Amount, &s.Price.Currency, &category, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return OfferedService{}, err
	}
	if description != nil {
		s.Description = *description
	}
	if model != nil {
		s.Price.Model = PriceModel(*model)
	}
	s.CategoryID = cloneIDPtr(category)
	s.Status = ServiceStatus(status)
	return s, nil
}

func idArg(id *ID) any {
	if id == nil {
		return nil
	}
	return *id
}
