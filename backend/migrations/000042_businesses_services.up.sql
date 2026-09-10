CREATE TABLE businesses.services (
    id UUID PRIMARY KEY,
    business_id UUID NOT NULL REFERENCES businesses.profiles (id),
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL,
    price_model TEXT,
    price_amount NUMERIC,
    price_currency TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT businesses_services_status_check CHECK (
        status IN ('draft', 'active', 'paused', 'closed')
    ),
    CONSTRAINT businesses_services_title_not_blank CHECK (
        char_length(btrim(title)) > 0
    ),
    CONSTRAINT businesses_services_price_model_check CHECK (
        price_model IS NULL OR price_model IN ('fixed', 'starting_from', 'quote_required')
    ),
    CONSTRAINT businesses_services_price_pair_check CHECK (
        (price_amount IS NULL AND price_currency IS NULL)
        OR (price_amount IS NOT NULL AND price_currency IS NOT NULL)
    ),
    CONSTRAINT businesses_services_price_model_amount_check CHECK (
        ((price_model IS NULL OR price_model = 'quote_required') AND price_amount IS NULL)
        OR (price_model IN ('fixed', 'starting_from') AND price_amount IS NOT NULL)
    ),
    CONSTRAINT businesses_services_price_currency_check CHECK (
        price_currency IS NULL OR price_currency ~ '^[A-Z]{3}$'
    ),
    CONSTRAINT businesses_services_updated_not_before_created CHECK (updated_at >= created_at)
);

CREATE INDEX businesses_services_business_id_status_idx
    ON businesses.services (business_id, status);

CREATE INDEX businesses_services_status_idx
    ON businesses.services (status);

CREATE INDEX businesses_services_business_created_id_idx
    ON businesses.services (business_id, created_at, id);
