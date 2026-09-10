CREATE SCHEMA payments;

CREATE TABLE payments.payments (
    id UUID PRIMARY KEY,
    transaction_id UUID NOT NULL,
    payer_user_id UUID NOT NULL,
    payee_user_id UUID NOT NULL,
    amount NUMERIC NOT NULL,
    currency TEXT NOT NULL,
    status TEXT NOT NULL,
    provider TEXT,
    provider_reference TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    authorized_at TIMESTAMPTZ,
    captured_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    CONSTRAINT payments_payments_status_check CHECK (
        status IN ('pending', 'authorized', 'captured', 'failed', 'cancelled')
    ),
    CONSTRAINT payments_payments_currency_check CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT payments_payments_updated_not_before_created CHECK (updated_at >= created_at),
    CONSTRAINT payments_payments_distinct_parties CHECK (payer_user_id <> payee_user_id),
    CONSTRAINT payments_payments_status_timestamps_check CHECK (
        (status = 'pending'
            AND authorized_at IS NULL
            AND captured_at IS NULL
            AND cancelled_at IS NULL
            AND failed_at IS NULL)
        OR (status = 'authorized'
            AND authorized_at IS NOT NULL
            AND captured_at IS NULL
            AND cancelled_at IS NULL
            AND failed_at IS NULL
            AND authorized_at >= created_at)
        OR (status = 'captured'
            AND authorized_at IS NOT NULL
            AND captured_at IS NOT NULL
            AND cancelled_at IS NULL
            AND failed_at IS NULL
            AND captured_at >= authorized_at)
        OR (status = 'cancelled'
            AND cancelled_at IS NOT NULL
            AND captured_at IS NULL
            AND failed_at IS NULL
            AND cancelled_at >= created_at)
        OR (status = 'failed'
            AND failed_at IS NOT NULL
            AND captured_at IS NULL
            AND cancelled_at IS NULL
            AND failed_at >= created_at)
    )
);

CREATE UNIQUE INDEX payments_payments_one_per_transaction_idx
    ON payments.payments (transaction_id);

CREATE UNIQUE INDEX payments_payments_provider_reference_uidx
    ON payments.payments (provider_reference)
    WHERE provider_reference IS NOT NULL;

CREATE INDEX payments_payments_payer_created_id_idx
    ON payments.payments (payer_user_id, created_at DESC, id);

CREATE INDEX payments_payments_payee_created_id_idx
    ON payments.payments (payee_user_id, created_at DESC, id);
