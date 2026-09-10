CREATE TABLE identity.passkey_credentials (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES identity.users (id),
    credential_id BYTEA NOT NULL,
    public_key BYTEA NOT NULL,
    sign_count BIGINT NOT NULL,
    backup_eligible BOOLEAN NOT NULL,
    backup_state BOOLEAN NOT NULL,
    transports TEXT[],
    created_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT identity_passkey_credentials_credential_id_unique UNIQUE (credential_id),
    CONSTRAINT identity_passkey_credentials_credential_id_nonempty CHECK (octet_length(credential_id) > 0),
    CONSTRAINT identity_passkey_credentials_public_key_nonempty CHECK (octet_length(public_key) > 0),
    CONSTRAINT identity_passkey_credentials_sign_count_nonneg CHECK (sign_count >= 0)
);

CREATE INDEX identity_passkey_credentials_user_id_idx ON identity.passkey_credentials (user_id);
CREATE INDEX identity_passkey_credentials_user_active_idx ON identity.passkey_credentials (user_id)
    WHERE revoked_at IS NULL;
