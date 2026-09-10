package listings

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/platform/db"
)

var _ listingStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

const listingSelectCols = `id, owner_user_id, status, category_id, category_schema_version,
	title, description, price_amount::text, price_currency, attributes,
	created_at, updated_at, published_at, archived_at, moderation_state`

func (p *PostgresStore) Create(ctx context.Context, listing Listing) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := listing.Validate(); err != nil {
		return err
	}
	attrs, err := marshalAttributes(listing.Attributes)
	if err != nil {
		return err
	}
	_, err = p.db.Exec(ctx, `
		INSERT INTO listings.listings (
			id, owner_user_id, status, category_id, category_schema_version,
			title, description, price_amount, price_currency, attributes,
			created_at, updated_at, published_at, archived_at, moderation_state
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		listing.ID, listing.OwnerUserID, string(listing.Status), listing.CategoryID, listing.CategorySchemaVersion,
		listing.Title, listing.Description, listing.PriceAmount, listing.PriceCurrency, attrs,
		listing.CreatedAt, listing.UpdatedAt, listing.PublishedAt, listing.ArchivedAt, string(listing.ModerationState.Normalized()),
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, id ID) (Listing, error) {
	if p == nil || p.db == nil {
		return Listing{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+listingSelectCols+`
		FROM listings.listings
		WHERE id = $1`, id)
	got, err := scanListing(row)
	if err != nil {
		return Listing{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) Update(ctx context.Context, listing Listing, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := listing.Validate(); err != nil {
		return err
	}
	attrs, err := marshalAttributes(listing.Attributes)
	if err != nil {
		return err
	}
	n, err := p.db.Exec(ctx, `
		UPDATE listings.listings SET
			status = $2,
			category_id = $3,
			category_schema_version = $4,
			title = $5,
			description = $6,
			price_amount = $7,
			price_currency = $8,
			attributes = $9,
			updated_at = $10,
			published_at = $11,
			archived_at = $12,
			moderation_state = $13
		WHERE id = $1 AND updated_at = $14`,
		listing.ID, string(listing.Status), listing.CategoryID, listing.CategorySchemaVersion,
		listing.Title, listing.Description, listing.PriceAmount, listing.PriceCurrency, attrs,
		listing.UpdatedAt, listing.PublishedAt, listing.ArchivedAt, string(listing.ModerationState.Normalized()), expectedUpdatedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		_, getErr := p.Get(ctx, listing.ID)
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

func (p *PostgresStore) ListByOwner(ctx context.Context, ownerUserID ID) ([]Listing, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+listingSelectCols+`
		FROM listings.listings
		WHERE owner_user_id = $1
		ORDER BY created_at DESC, id ASC`, ownerUserID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Listing, 0)
	for rows.Next() {
		listing, err := scanListing(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, listing)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func scanListing(row interface {
	Scan(dest ...any) error
}) (Listing, error) {
	var l Listing
	var status string
	var moderationState string
	var attrs []byte
	if err := row.Scan(
		&l.ID, &l.OwnerUserID, &status, &l.CategoryID, &l.CategorySchemaVersion,
		&l.Title, &l.Description, &l.PriceAmount, &l.PriceCurrency, &attrs,
		&l.CreatedAt, &l.UpdatedAt, &l.PublishedAt, &l.ArchivedAt, &moderationState,
	); err != nil {
		return Listing{}, err
	}
	l.Status = Status(status)
	l.ModerationState = ModerationState(moderationState)
	parsed, err := unmarshalAttributes(attrs)
	if err != nil {
		return Listing{}, err
	}
	l.Attributes = parsed
	return l, nil
}

func marshalAttributes(attrs Attributes) ([]byte, error) {
	if attrs == nil {
		attrs = Attributes{}
	}
	if err := ValidateAttributesShape(attrs); err != nil {
		return nil, err
	}
	b, err := json.Marshal(attrs)
	if err != nil {
		return nil, errInvalidAttributes
	}
	return b, nil
}

func unmarshalAttributes(raw []byte) (Attributes, error) {
	if len(raw) == 0 {
		return Attributes{}, nil
	}
	var attrs Attributes
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, errInvalidAttributes
	}
	if attrs == nil {
		attrs = Attributes{}
	}
	if err := ValidateAttributesShape(attrs); err != nil {
		return nil, err
	}
	return attrs, nil
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
