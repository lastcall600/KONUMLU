ALTER TABLE identity.users
    DROP CONSTRAINT IF EXISTS identity_users_session_epoch_nonnegative;

ALTER TABLE identity.users
    DROP COLUMN IF EXISTS session_epoch;
