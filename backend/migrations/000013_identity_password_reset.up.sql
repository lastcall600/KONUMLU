ALTER TABLE identity.verification_challenges
    DROP CONSTRAINT identity_verification_challenges_purpose_check;

ALTER TABLE identity.verification_challenges
    ADD CONSTRAINT identity_verification_challenges_purpose_check
    CHECK (purpose IN ('signup', 'password_reset'));

CREATE TABLE identity.password_reset_proofs (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES identity.users (id),
    challenge_id UUID NOT NULL REFERENCES identity.verification_challenges (id),
    purpose TEXT NOT NULL,
    token_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    CONSTRAINT identity_password_reset_proofs_purpose_check CHECK (purpose IN ('password_reset')),
    CONSTRAINT identity_password_reset_proofs_token_hash_len CHECK (octet_length(token_hash) = 32),
    CONSTRAINT identity_password_reset_proofs_expires_after_created CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX identity_password_reset_proofs_token_hash_uidx
    ON identity.password_reset_proofs (token_hash);

CREATE UNIQUE INDEX identity_password_reset_proofs_challenge_id_uidx
    ON identity.password_reset_proofs (challenge_id);
