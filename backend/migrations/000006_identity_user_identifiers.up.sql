CREATE TABLE identity.user_identifiers (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES identity.users (id),
    kind TEXT NOT NULL,
    value_canonical TEXT NOT NULL,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT identity_user_identifiers_kind_check CHECK (kind IN ('email', 'phone')),
    CONSTRAINT identity_user_identifiers_value_nonempty CHECK (char_length(value_canonical) > 0)
);

CREATE UNIQUE INDEX identity_user_identifiers_kind_canonical_active_uidx
    ON identity.user_identifiers (kind, value_canonical)
    WHERE revoked_at IS NULL;

CREATE INDEX identity_user_identifiers_user_id_idx ON identity.user_identifiers (user_id);
