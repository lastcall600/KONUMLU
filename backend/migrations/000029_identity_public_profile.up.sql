-- Identity-owned public profile identity fields.
-- public_profile_id is an app-generated opaque UUID, never the internal user id.
-- No email, phone, credentials, legal identity, or session data.

CREATE TABLE identity.public_profiles (
    user_id UUID PRIMARY KEY REFERENCES identity.users (id),
    public_profile_id UUID NOT NULL,
    display_name TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT identity_public_profiles_public_profile_id_unique UNIQUE (public_profile_id),
    CONSTRAINT identity_public_profiles_display_name_len CHECK (
        display_name IS NULL OR char_length(display_name) <= 80
    )
);
