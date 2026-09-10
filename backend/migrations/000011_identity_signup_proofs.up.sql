CREATE TABLE identity.signup_proofs (
    id UUID PRIMARY KEY,
    challenge_id UUID NOT NULL REFERENCES identity.verification_challenges (id),
    kind TEXT NOT NULL,
    destination_canonical TEXT NOT NULL,
    purpose TEXT NOT NULL,
    token_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    CONSTRAINT identity_signup_proofs_kind_check CHECK (kind IN ('email', 'phone')),
    CONSTRAINT identity_signup_proofs_purpose_check CHECK (purpose IN ('signup')),
    CONSTRAINT identity_signup_proofs_destination_nonempty CHECK (char_length(destination_canonical) > 0),
    CONSTRAINT identity_signup_proofs_token_hash_len CHECK (octet_length(token_hash) = 32),
    CONSTRAINT identity_signup_proofs_expires_after_created CHECK (expires_at > created_at)
);

CREATE UNIQUE INDEX identity_signup_proofs_token_hash_uidx
    ON identity.signup_proofs (token_hash);

CREATE UNIQUE INDEX identity_signup_proofs_challenge_id_uidx
    ON identity.signup_proofs (challenge_id);
