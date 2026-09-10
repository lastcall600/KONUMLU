CREATE TABLE identity.password_credentials (
    user_id UUID PRIMARY KEY REFERENCES identity.users (id),
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    disabled_at TIMESTAMPTZ,
    CONSTRAINT identity_password_credentials_hash_nonempty CHECK (char_length(password_hash) > 0)
);
