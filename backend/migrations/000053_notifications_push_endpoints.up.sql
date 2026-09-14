-- Additive push endpoint registry. Does not alter 000052 policy tables.
-- No Identity FKs, ENUMs, or plaintext endpoint material.

CREATE TABLE notifications.push_endpoints (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    channel TEXT NOT NULL,
    platform TEXT NOT NULL,
    provider TEXT NOT NULL,
    endpoint_hash BYTEA NOT NULL,
    endpoint_key_id TEXT NOT NULL,
    endpoint_nonce BYTEA NOT NULL,
    endpoint_ciphertext BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT notifications_push_endpoints_channel_check CHECK (
        channel IN ('web_push', 'mobile_push')
    ),
    CONSTRAINT notifications_push_endpoints_platform_check CHECK (
        platform IN ('web', 'android', 'ios')
    ),
    CONSTRAINT notifications_push_endpoints_provider_check CHECK (
        provider IN ('webpush', 'fcm', 'apns')
    ),
    CONSTRAINT notifications_push_endpoints_combo_check CHECK (
        (channel = 'web_push' AND platform = 'web' AND provider = 'webpush')
        OR (channel = 'mobile_push' AND platform = 'android' AND provider = 'fcm')
        OR (channel = 'mobile_push' AND platform = 'ios' AND provider = 'apns')
    ),
    CONSTRAINT notifications_push_endpoints_hash_len CHECK (octet_length(endpoint_hash) = 32),
    CONSTRAINT notifications_push_endpoints_key_id_nonempty CHECK (
        char_length(endpoint_key_id) BETWEEN 1 AND 64
    ),
    CONSTRAINT notifications_push_endpoints_nonce_len CHECK (octet_length(endpoint_nonce) = 12),
    CONSTRAINT notifications_push_endpoints_ciphertext_min CHECK (octet_length(endpoint_ciphertext) >= 16),
    CONSTRAINT notifications_push_endpoints_updated_not_before_created
        CHECK (updated_at >= created_at),
    CONSTRAINT notifications_push_endpoints_last_seen_not_before_created
        CHECK (last_seen_at >= created_at),
    CONSTRAINT notifications_push_endpoints_revoked_not_before_created
        CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE UNIQUE INDEX notifications_push_endpoints_active_hash_uidx
    ON notifications.push_endpoints (endpoint_hash)
    WHERE revoked_at IS NULL;

CREATE INDEX notifications_push_endpoints_user_active_idx
    ON notifications.push_endpoints (user_id, last_seen_at DESC, id DESC)
    WHERE revoked_at IS NULL;

CREATE INDEX notifications_push_endpoints_user_id_idx
    ON notifications.push_endpoints (user_id, created_at DESC, id DESC);
