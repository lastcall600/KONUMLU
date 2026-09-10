CREATE SCHEMA deliveries;

CREATE TABLE deliveries.deliveries (
    id UUID PRIMARY KEY,
    transaction_id UUID NOT NULL,
    requester_user_id UUID NOT NULL,
    provider_user_id UUID NOT NULL,
    eligibility TEXT NOT NULL,
    status TEXT NOT NULL,
    method TEXT,
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    dispatched_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    CONSTRAINT deliveries_deliveries_eligibility_check CHECK (
        eligibility = 'delivery_required'
    ),
    CONSTRAINT deliveries_deliveries_status_check CHECK (
        status IN ('pending', 'ready', 'in_transit', 'delivered', 'cancelled')
    ),
    CONSTRAINT deliveries_deliveries_method_check CHECK (
        method IS NULL OR method IN ('handoff', 'courier', 'pickup')
    ),
    CONSTRAINT deliveries_deliveries_note_len_check CHECK (
        note IS NULL OR char_length(note) BETWEEN 1 AND 500
    ),
    CONSTRAINT deliveries_deliveries_updated_not_before_created CHECK (updated_at >= created_at),
    CONSTRAINT deliveries_deliveries_distinct_parties CHECK (requester_user_id <> provider_user_id),
    CONSTRAINT deliveries_deliveries_status_timestamps_check CHECK (
        (status = 'pending'
            AND dispatched_at IS NULL
            AND delivered_at IS NULL
            AND cancelled_at IS NULL)
        OR (status = 'ready'
            AND dispatched_at IS NULL
            AND delivered_at IS NULL
            AND cancelled_at IS NULL)
        OR (status = 'in_transit'
            AND dispatched_at IS NOT NULL
            AND delivered_at IS NULL
            AND cancelled_at IS NULL
            AND dispatched_at >= created_at
            AND (method IS NULL OR method = 'courier'))
        OR (status = 'delivered'
            AND delivered_at IS NOT NULL
            AND cancelled_at IS NULL
            AND delivered_at >= created_at
            AND (
                ((method IS NULL OR method = 'courier')
                    AND dispatched_at IS NOT NULL
                    AND delivered_at >= dispatched_at)
                OR (method IN ('handoff', 'pickup') AND dispatched_at IS NULL)
            ))
        OR (status = 'cancelled'
            AND cancelled_at IS NOT NULL
            AND delivered_at IS NULL
            AND cancelled_at >= created_at)
    )
);

CREATE UNIQUE INDEX deliveries_deliveries_one_per_transaction_idx
    ON deliveries.deliveries (transaction_id);

CREATE INDEX deliveries_deliveries_requester_created_id_idx
    ON deliveries.deliveries (requester_user_id, created_at DESC, id);

CREATE INDEX deliveries_deliveries_provider_created_id_idx
    ON deliveries.deliveries (provider_user_id, created_at DESC, id);
