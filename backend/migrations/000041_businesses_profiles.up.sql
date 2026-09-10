CREATE SCHEMA businesses;

CREATE TABLE businesses.profiles (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL,
    display_name TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT businesses_profiles_status_check CHECK (
        status IN ('draft', 'active', 'suspended', 'closed')
    ),
    CONSTRAINT businesses_profiles_display_name_not_blank CHECK (
        char_length(btrim(display_name)) > 0
    ),
    CONSTRAINT businesses_profiles_updated_not_before_created CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX businesses_profiles_owner_user_id_uidx
    ON businesses.profiles (owner_user_id);

CREATE INDEX businesses_profiles_status_idx
    ON businesses.profiles (status);
