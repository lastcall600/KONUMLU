CREATE TABLE identity.verification_material (
    challenge_id UUID PRIMARY KEY REFERENCES identity.verification_challenges (id) ON DELETE CASCADE,
    key_id TEXT NOT NULL,
    nonce BYTEA NOT NULL,
    ciphertext BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    destroyed_at TIMESTAMPTZ,
    CONSTRAINT identity_verification_material_key_id_nonempty CHECK (char_length(key_id) > 0),
    CONSTRAINT identity_verification_material_destroyed_not_before_created CHECK (
        destroyed_at IS NULL OR destroyed_at >= created_at
    ),
    CONSTRAINT identity_verification_material_active_or_destroyed CHECK (
        (
            destroyed_at IS NULL
            AND octet_length(nonce) = 12
            AND octet_length(ciphertext) >= 16
        )
        OR (
            destroyed_at IS NOT NULL
            AND octet_length(nonce) = 0
            AND octet_length(ciphertext) = 0
        )
    )
);
