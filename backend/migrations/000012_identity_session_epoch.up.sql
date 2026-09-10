ALTER TABLE identity.users
    ADD COLUMN session_epoch BIGINT NOT NULL DEFAULT 0;

ALTER TABLE identity.users
    ADD CONSTRAINT identity_users_session_epoch_nonnegative
    CHECK (session_epoch >= 0);
