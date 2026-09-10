CREATE SCHEMA IF NOT EXISTS platform;

CREATE TABLE platform.outbox_events (
    id UUID PRIMARY KEY,
    event_type TEXT NOT NULL,
    event_version INTEGER NOT NULL,
    aggregate_type TEXT,
    aggregate_id TEXT,
    payload JSONB NOT NULL,
    idempotency_key TEXT,
    correlation_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ,
    claim_until TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error_class TEXT,
    CONSTRAINT platform_outbox_events_event_type_nonempty CHECK (char_length(event_type) > 0),
    CONSTRAINT platform_outbox_events_event_version_positive CHECK (event_version > 0),
    CONSTRAINT platform_outbox_events_payload_structured CHECK (jsonb_typeof(payload) IN ('object', 'array')),
    CONSTRAINT platform_outbox_events_attempts_nonneg CHECK (attempts >= 0),
    CONSTRAINT platform_outbox_events_available_not_before_created CHECK (available_at >= created_at),
    CONSTRAINT platform_outbox_events_claim_window CHECK (
        (claimed_at IS NULL AND claim_until IS NULL)
        OR (claimed_at IS NOT NULL AND claim_until IS NOT NULL AND claim_until > claimed_at)
    )
);

CREATE UNIQUE INDEX platform_outbox_events_idempotency_key_uidx
    ON platform.outbox_events (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX platform_outbox_events_claim_idx
    ON platform.outbox_events (available_at, id)
    WHERE completed_at IS NULL;
