-- Transaction/delivery verification flows. Listing inspection remains appointment-scoped.
-- Existing listing_inspection rows stay valid: appointment_id remains set, flow_id stays NULL.

CREATE TABLE verified.verification_flows (
    id UUID PRIMARY KEY,
    listing_id UUID NOT NULL,
    requester_user_id UUID NOT NULL,
    provider_user_id UUID NOT NULL,
    interaction_type TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT verification_flows_participants_distinct CHECK (requester_user_id <> provider_user_id),
    CONSTRAINT verification_flows_type_allowed CHECK (interaction_type IN ('transaction', 'delivery')),
    CONSTRAINT verification_flows_status_allowed CHECK (status IN ('open', 'completed'))
);

ALTER TABLE verified.verification_challenges
    ALTER COLUMN appointment_id DROP NOT NULL,
    ADD COLUMN flow_id UUID REFERENCES verified.verification_flows (id);

ALTER TABLE verified.verification_challenges
    ADD CONSTRAINT verification_challenges_scope CHECK (
        (appointment_id IS NOT NULL AND flow_id IS NULL)
        OR (appointment_id IS NULL AND flow_id IS NOT NULL)
    );

ALTER TABLE verified.verified_interactions
    DROP CONSTRAINT verified_interactions_appointment_id_key,
    DROP CONSTRAINT verified_interactions_type_allowed,
    ALTER COLUMN appointment_id DROP NOT NULL,
    ADD COLUMN flow_id UUID REFERENCES verified.verification_flows (id);

ALTER TABLE verified.verified_interactions
    ADD CONSTRAINT verified_interactions_type_allowed CHECK (
        interaction_type IN ('listing_inspection', 'transaction', 'delivery')
    ),
    ADD CONSTRAINT verified_interactions_scope_consistent CHECK (
        (
            interaction_type = 'listing_inspection'
            AND appointment_id IS NOT NULL
            AND flow_id IS NULL
        ) OR (
            interaction_type IN ('transaction', 'delivery')
            AND appointment_id IS NULL
            AND flow_id IS NOT NULL
        )
    );

CREATE UNIQUE INDEX verified_interactions_appointment_uidx
    ON verified.verified_interactions (appointment_id)
    WHERE appointment_id IS NOT NULL;

CREATE UNIQUE INDEX verified_interactions_flow_uidx
    ON verified.verified_interactions (flow_id)
    WHERE flow_id IS NOT NULL;

CREATE INDEX verification_challenges_flow_idx
    ON verified.verification_challenges (flow_id, created_at DESC, id DESC)
    WHERE flow_id IS NOT NULL;

CREATE INDEX verification_flows_requester_updated_idx
    ON verified.verification_flows (requester_user_id, updated_at DESC, id DESC);

CREATE INDEX verification_flows_provider_updated_idx
    ON verified.verification_flows (provider_user_id, updated_at DESC, id DESC);
