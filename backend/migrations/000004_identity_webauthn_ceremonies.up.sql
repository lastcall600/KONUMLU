CREATE TABLE identity.webauthn_ceremonies (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL,
    user_id UUID REFERENCES identity.users (id),
    token_hash BYTEA NOT NULL,
    session_data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    CONSTRAINT identity_webauthn_ceremonies_kind_check CHECK (kind IN ('registration', 'authentication')),
    CONSTRAINT identity_webauthn_ceremonies_token_hash_unique UNIQUE (token_hash),
    CONSTRAINT identity_webauthn_ceremonies_token_hash_len CHECK (octet_length(token_hash) = 32),
    CONSTRAINT identity_webauthn_ceremonies_session_data_nonempty CHECK (octet_length(session_data) > 0),
    CONSTRAINT identity_webauthn_ceremonies_expires_after_created CHECK (expires_at > created_at)
);

CREATE INDEX identity_webauthn_ceremonies_user_id_idx ON identity.webauthn_ceremonies (user_id);
