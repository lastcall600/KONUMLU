CREATE TABLE identity.verification_challenges (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL,
    purpose TEXT NOT NULL,
    destination_canonical TEXT NOT NULL,
    token_hash BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    failed_attempts INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL,
    CONSTRAINT identity_verification_challenges_kind_check CHECK (kind IN ('email', 'phone')),
    CONSTRAINT identity_verification_challenges_purpose_check CHECK (purpose IN ('signup')),
    CONSTRAINT identity_verification_challenges_destination_nonempty CHECK (char_length(destination_canonical) > 0),
    CONSTRAINT identity_verification_challenges_token_hash_len CHECK (octet_length(token_hash) = 32),
    CONSTRAINT identity_verification_challenges_expires_after_created CHECK (expires_at > created_at),
    CONSTRAINT identity_verification_challenges_failed_attempts_nonneg CHECK (failed_attempts >= 0),
    CONSTRAINT identity_verification_challenges_max_attempts_positive CHECK (max_attempts > 0),
    CONSTRAINT identity_verification_challenges_failed_lte_max CHECK (failed_attempts <= max_attempts)
);

CREATE INDEX identity_verification_challenges_kind_destination_idx
    ON identity.verification_challenges (kind, destination_canonical);
