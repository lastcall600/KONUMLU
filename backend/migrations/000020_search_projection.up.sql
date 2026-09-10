CREATE SCHEMA search;

-- Derived, rebuildable listing discovery documents. Canonical data remains
-- in owning domains. PostgreSQL FTS config (dictionaries, languages) is OPEN.
CREATE TABLE search.listing_documents (
    listing_id UUID PRIMARY KEY,
    status TEXT NOT NULL,
    category_id UUID NOT NULL,
    category_schema_version INTEGER NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    price_amount NUMERIC,
    price_currency TEXT,
    attributes JSONB NOT NULL,
    point geography(Point, 4326),
    catalog_location_id UUID,
    published_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(description, '')), 'B')
    ) STORED,
    CONSTRAINT search_listing_documents_schema_version_positive CHECK (category_schema_version > 0),
    CONSTRAINT search_listing_documents_price_pair_check CHECK (
        (price_amount IS NULL AND price_currency IS NULL)
        OR (price_amount IS NOT NULL AND price_currency IS NOT NULL)
    ),
    CONSTRAINT search_listing_documents_price_currency_check CHECK (
        price_currency IS NULL OR price_currency ~ '^[A-Z]{3}$'
    ),
    CONSTRAINT search_listing_documents_attributes_object_check CHECK (jsonb_typeof(attributes) = 'object')
);

CREATE INDEX search_listing_documents_search_vector_idx
    ON search.listing_documents USING GIN (search_vector);

CREATE INDEX search_listing_documents_point_idx
    ON search.listing_documents USING GIST (point);

CREATE INDEX search_listing_documents_category_id_idx
    ON search.listing_documents (category_id);
