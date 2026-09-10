DROP INDEX IF EXISTS verified.verification_flows_provider_updated_idx;
DROP INDEX IF EXISTS verified.verification_flows_requester_updated_idx;
DROP INDEX IF EXISTS verified.verification_challenges_flow_idx;
DROP INDEX IF EXISTS verified.verified_interactions_flow_uidx;
DROP INDEX IF EXISTS verified.verified_interactions_appointment_uidx;

ALTER TABLE verified.verified_interactions
    DROP CONSTRAINT IF EXISTS verified_interactions_scope_consistent,
    DROP CONSTRAINT IF EXISTS verified_interactions_type_allowed;

ALTER TABLE verified.verified_interactions
    DROP COLUMN IF EXISTS flow_id;

ALTER TABLE verified.verified_interactions
    ALTER COLUMN appointment_id SET NOT NULL,
    ADD CONSTRAINT verified_interactions_appointment_id_key UNIQUE (appointment_id),
    ADD CONSTRAINT verified_interactions_type_allowed CHECK (interaction_type IN ('listing_inspection'));

ALTER TABLE verified.verification_challenges
    DROP CONSTRAINT IF EXISTS verification_challenges_scope;

ALTER TABLE verified.verification_challenges
    DROP COLUMN IF EXISTS flow_id;

ALTER TABLE verified.verification_challenges
    ALTER COLUMN appointment_id SET NOT NULL;

DROP TABLE IF EXISTS verified.verification_flows;
