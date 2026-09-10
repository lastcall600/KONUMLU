ALTER TABLE identity.public_profiles
    DROP CONSTRAINT IF EXISTS identity_public_profiles_moderation_state_check;

ALTER TABLE identity.public_profiles
    DROP COLUMN IF EXISTS moderation_state;
