CREATE SCHEMA listings;

CREATE TABLE listings.listings (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL,
    status TEXT NOT NULL,
    category_id UUID NOT NULL,
    category_schema_version INTEGER NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    price_amount NUMERIC,
    price_currency TEXT,
    attributes JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    CONSTRAINT listings_listings_status_check CHECK (
        status IN ('draft', 'ready', 'verification_pending', 'published', 'archived')
    ),
    CONSTRAINT listings_listings_schema_version_positive CHECK (category_schema_version > 0),
    CONSTRAINT listings_listings_price_pair_check CHECK (
        (price_amount IS NULL AND price_currency IS NULL)
        OR (price_amount IS NOT NULL AND price_currency IS NOT NULL)
    ),
    CONSTRAINT listings_listings_price_currency_check CHECK (
        price_currency IS NULL OR price_currency ~ '^[A-Z]{3}$'
    ),
    CONSTRAINT listings_listings_attributes_object_check CHECK (jsonb_typeof(attributes) = 'object'),
    CONSTRAINT listings_listings_updated_not_before_created CHECK (updated_at >= created_at)
);

CREATE INDEX listings_listings_owner_user_id_idx ON listings.listings (owner_user_id);
