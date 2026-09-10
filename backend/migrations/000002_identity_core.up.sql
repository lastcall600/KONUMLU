CREATE SCHEMA identity;

CREATE TABLE identity.users (
    id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    disabled_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE identity.devices (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES identity.users (id),
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX identity_devices_user_id_idx ON identity.devices (user_id);

CREATE TABLE identity.sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES identity.users (id),
    device_id UUID REFERENCES identity.devices (id),
    token_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    idle_expires_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT identity_sessions_token_hash_unique UNIQUE (token_hash),
    CONSTRAINT identity_sessions_token_hash_len CHECK (octet_length(token_hash) = 32)
);

CREATE INDEX identity_sessions_user_id_idx ON identity.sessions (user_id);
