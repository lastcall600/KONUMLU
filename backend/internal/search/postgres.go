package search

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"backend/internal/platform/db"
)

var _ documentStore = (*PostgresStore)(nil)
var _ listingQueryStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

const listingDocumentSelectCols = `listing_id, status, category_id, category_schema_version,
	title, description, price_amount::text, price_currency, attributes,
	CASE WHEN point IS NULL THEN NULL ELSE ST_Y(point::geometry) END,
	CASE WHEN point IS NULL THEN NULL ELSE ST_X(point::geometry) END,
	catalog_location_id, published_at, updated_at`

func (p *PostgresStore) Upsert(ctx context.Context, doc ListingDocument) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	attrs, err := marshalAttributes(doc.Attributes)
	if err != nil {
		return err
	}
	_, err = p.db.Exec(ctx, `
		INSERT INTO search.listing_documents (
			listing_id, status, category_id, category_schema_version,
			title, description, price_amount, price_currency, attributes,
			point, catalog_location_id, published_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,
			CASE WHEN $10::float8 IS NULL OR $11::float8 IS NULL THEN NULL
			     ELSE ST_SetSRID(ST_MakePoint($11, $10), 4326)::geography END,
			$12,$13,$14
		)
		ON CONFLICT (listing_id) DO UPDATE SET
			status = EXCLUDED.status,
			category_id = EXCLUDED.category_id,
			category_schema_version = EXCLUDED.category_schema_version,
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			price_amount = EXCLUDED.price_amount,
			price_currency = EXCLUDED.price_currency,
			attributes = EXCLUDED.attributes,
			point = EXCLUDED.point,
			catalog_location_id = EXCLUDED.catalog_location_id,
			published_at = EXCLUDED.published_at,
			updated_at = EXCLUDED.updated_at`,
		doc.ListingID, doc.Status, doc.CategoryID, doc.CategorySchemaVersion,
		doc.Title, doc.Description, doc.PriceAmount, doc.PriceCurrency, attrs,
		doc.Latitude, doc.Longitude, catalogArg(doc.CatalogLocationID),
		doc.PublishedAt, doc.UpdatedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, listingID ID) (ListingDocument, error) {
	if p == nil || p.db == nil {
		return ListingDocument{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+listingDocumentSelectCols+`
		FROM search.listing_documents
		WHERE listing_id = $1`, listingID)
	got, err := scanDocument(row)
	if err != nil {
		return ListingDocument{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) Remove(ctx context.Context, listingID ID) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, `DELETE FROM search.listing_documents WHERE listing_id = $1`, listingID)
	return mapDBErr(err)
}

func (p *PostgresStore) Search(ctx context.Context, q NormalizedQuery) ([]Hit, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	sql, args := buildSearchSQL(q)
	rows, err := p.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		hit, err := scanHit(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return hits, nil
}

func buildSearchSQL(q NormalizedQuery) (string, []any) {
	var b strings.Builder
	args := make([]any, 0, 16)
	add := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	b.WriteString(`SELECT `)
	b.WriteString(listingDocumentSelectCols)
	b.WriteString(`, `)
	if q.HasText {
		b.WriteString(`ts_rank(search_vector, plainto_tsquery('simple', `)
		b.WriteString(add(q.Q))
		b.WriteString(`))`)
	} else {
		b.WriteString(`0::float8`)
	}
	b.WriteString(` FROM search.listing_documents WHERE status = 'published'`)
	if q.CategoryID != nil {
		b.WriteString(` AND category_id = `)
		b.WriteString(add(*q.CategoryID))
	}
	if q.MinPrice != nil {
		b.WriteString(` AND price_amount >= `)
		b.WriteString(add(q.MinPrice.FloatString(8)))
	}
	if q.MaxPrice != nil {
		b.WriteString(` AND price_amount <= `)
		b.WriteString(add(q.MaxPrice.FloatString(8)))
	}
	if q.Currency != nil {
		b.WriteString(` AND price_currency = `)
		b.WriteString(add(*q.Currency))
	}
	if q.Viewport != nil {
		west := add(q.Viewport.West)
		south := add(q.Viewport.South)
		east := add(q.Viewport.East)
		north := add(q.Viewport.North)
		b.WriteString(` AND point IS NOT NULL AND ST_Intersects(point, ST_MakeEnvelope(`)
		b.WriteString(west)
		b.WriteString(`, `)
		b.WriteString(south)
		b.WriteString(`, `)
		b.WriteString(east)
		b.WriteString(`, `)
		b.WriteString(north)
		b.WriteString(`, 4326)::geography)`)
	}
	if q.HasText {
		b.WriteString(` AND search_vector @@ plainto_tsquery('simple', `)
		b.WriteString(add(q.Q))
		b.WriteString(`)`)
	} else {
		b.WriteString(` AND published_at IS NOT NULL`)
	}
	if q.Cursor != nil {
		if q.HasText {
			rankP := add(q.Cursor.Rank)
			idP := add(q.Cursor.ListingID)
			q1 := add(q.Q)
			q2 := add(q.Q)
			b.WriteString(` AND (ts_rank(search_vector, plainto_tsquery('simple', `)
			b.WriteString(q1)
			b.WriteString(`)) < `)
			b.WriteString(rankP)
			b.WriteString(` OR (ts_rank(search_vector, plainto_tsquery('simple', `)
			b.WriteString(q2)
			b.WriteString(`)) = `)
			b.WriteString(rankP)
			b.WriteString(` AND listing_id < `)
			b.WriteString(idP)
			b.WriteString(`))`)
		} else {
			pubP := add(q.Cursor.PublishedAt)
			idP := add(q.Cursor.ListingID)
			b.WriteString(` AND (published_at < `)
			b.WriteString(pubP)
			b.WriteString(` OR (published_at = `)
			b.WriteString(pubP)
			b.WriteString(` AND listing_id < `)
			b.WriteString(idP)
			b.WriteString(`))`)
		}
	}
	if q.HasText {
		b.WriteString(` ORDER BY ts_rank(search_vector, plainto_tsquery('simple', `)
		b.WriteString(add(q.Q))
		b.WriteString(`)) DESC, listing_id DESC`)
	} else {
		b.WriteString(` ORDER BY published_at DESC, listing_id DESC`)
	}
	b.WriteString(` LIMIT `)
	b.WriteString(add(q.Limit + 1))
	return b.String(), args
}

func scanHit(row interface {
	Scan(dest ...any) error
}) (Hit, error) {
	var d ListingDocument
	var attrs []byte
	var rank float64
	if err := row.Scan(
		&d.ListingID, &d.Status, &d.CategoryID, &d.CategorySchemaVersion,
		&d.Title, &d.Description, &d.PriceAmount, &d.PriceCurrency, &attrs,
		&d.Latitude, &d.Longitude, &d.CatalogLocationID, &d.PublishedAt, &d.UpdatedAt,
		&rank,
	); err != nil {
		return Hit{}, err
	}
	parsed, err := unmarshalAttributes(attrs)
	if err != nil {
		return Hit{}, err
	}
	d.Attributes = parsed
	return Hit{Doc: d, Rank: rank}, nil
}

func catalogArg(id *ID) any {
	if id == nil {
		return nil
	}
	return *id
}

func scanDocument(row interface {
	Scan(dest ...any) error
}) (ListingDocument, error) {
	var d ListingDocument
	var attrs []byte
	if err := row.Scan(
		&d.ListingID, &d.Status, &d.CategoryID, &d.CategorySchemaVersion,
		&d.Title, &d.Description, &d.PriceAmount, &d.PriceCurrency, &attrs,
		&d.Latitude, &d.Longitude, &d.CatalogLocationID, &d.PublishedAt, &d.UpdatedAt,
	); err != nil {
		return ListingDocument{}, err
	}
	parsed, err := unmarshalAttributes(attrs)
	if err != nil {
		return ListingDocument{}, err
	}
	d.Attributes = parsed
	return d, nil
}

func marshalAttributes(attrs map[string]any) ([]byte, error) {
	if attrs == nil {
		attrs = map[string]any{}
	}
	b, err := json.Marshal(attrs)
	if err != nil {
		return nil, errInvalidDoc
	}
	return b, nil
}

func unmarshalAttributes(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var attrs map[string]any
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, errInvalidDoc
	}
	if attrs == nil {
		attrs = map[string]any{}
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
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
