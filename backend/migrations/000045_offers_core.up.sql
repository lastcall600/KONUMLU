CREATE SCHEMA offers;

CREATE TABLE offers.offers (
    id UUID PRIMARY KEY,
    need_id UUID NOT NULL,
    provider_business_id UUID NOT NULL,
    service_id UUID NOT NULL,
    provider_user_id UUID NOT NULL,
    message TEXT,
    price_amount NUMERIC,
    price_currency TEXT,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT offers_offers_status_check CHECK (
        status IN ('submitted', 'withdrawn', 'accepted', 'rejected', 'expired')
    ),
    CONSTRAINT offers_offers_price_all_or_none CHECK (
        (price_amount IS NULL AND price_currency IS NULL)
        OR (price_amount IS NOT NULL AND price_currency IS NOT NULL)
    ),
    CONSTRAINT offers_offers_price_currency_check CHECK (
        price_currency IS NULL OR price_currency ~ '^[A-Z]{3}$'
    ),
    CONSTRAINT offers_offers_updated_not_before_created CHECK (updated_at >= created_at)
);

CREATE INDEX offers_offers_need_created_id_idx
    ON offers.offers (need_id, created_at DESC, id);

CREATE INDEX offers_offers_provider_created_id_idx
    ON offers.offers (provider_user_id, created_at DESC, id);

CREATE UNIQUE INDEX offers_offers_one_accepted_per_need_idx
    ON offers.offers (need_id)
    WHERE status = 'accepted';

CREATE UNIQUE INDEX offers_offers_one_submitted_per_need_service_idx
    ON offers.offers (need_id, service_id)
    WHERE status = 'submitted';
