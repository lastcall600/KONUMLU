CREATE SCHEMA transactions;

CREATE TABLE transactions.transactions (
    id UUID PRIMARY KEY,
    offer_id UUID NOT NULL,
    need_id UUID NOT NULL,
    requester_user_id UUID NOT NULL,
    provider_user_id UUID NOT NULL,
    provider_business_id UUID NOT NULL,
    service_id UUID NOT NULL,
    agreed_amount NUMERIC,
    agreed_currency TEXT,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    CONSTRAINT transactions_transactions_status_check CHECK (
        status IN ('pending', 'active', 'completed', 'cancelled')
    ),
    CONSTRAINT transactions_transactions_price_all_or_none CHECK (
        (agreed_amount IS NULL AND agreed_currency IS NULL)
        OR (agreed_amount IS NOT NULL AND agreed_currency IS NOT NULL)
    ),
    CONSTRAINT transactions_transactions_price_currency_check CHECK (
        agreed_currency IS NULL OR agreed_currency ~ '^[A-Z]{3}$'
    ),
    CONSTRAINT transactions_transactions_updated_not_before_created CHECK (updated_at >= created_at),
    CONSTRAINT transactions_transactions_distinct_parties CHECK (requester_user_id <> provider_user_id),
    CONSTRAINT transactions_transactions_completed_at_check CHECK (
        (status = 'completed' AND completed_at IS NOT NULL AND cancelled_at IS NULL AND completed_at >= created_at)
        OR (status <> 'completed' AND completed_at IS NULL)
    ),
    CONSTRAINT transactions_transactions_cancelled_at_check CHECK (
        (status = 'cancelled' AND cancelled_at IS NOT NULL AND completed_at IS NULL AND cancelled_at >= created_at)
        OR (status <> 'cancelled' AND cancelled_at IS NULL)
    )
);

CREATE UNIQUE INDEX transactions_transactions_one_per_offer_idx
    ON transactions.transactions (offer_id);

CREATE INDEX transactions_transactions_requester_created_id_idx
    ON transactions.transactions (requester_user_id, created_at DESC, id);

CREATE INDEX transactions_transactions_provider_created_id_idx
    ON transactions.transactions (provider_user_id, created_at DESC, id);
