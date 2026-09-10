DROP TABLE IF EXISTS identity.password_reset_proofs;

ALTER TABLE identity.verification_challenges
    DROP CONSTRAINT identity_verification_challenges_purpose_check;

ALTER TABLE identity.verification_challenges
    ADD CONSTRAINT identity_verification_challenges_purpose_check
    CHECK (purpose IN ('signup'));
