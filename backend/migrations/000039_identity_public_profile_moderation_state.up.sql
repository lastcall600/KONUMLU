-- Dedicated public-profile staff visibility. Independent of account disabled/deleted/session.
-- Existing rows stay publicly visible (none).

ALTER TABLE identity.public_profiles
    ADD COLUMN moderation_state TEXT NOT NULL DEFAULT 'none';

ALTER TABLE identity.public_profiles
    ADD CONSTRAINT identity_public_profiles_moderation_state_check CHECK (
        moderation_state IN ('none', 'restricted', 'removed')
    );
