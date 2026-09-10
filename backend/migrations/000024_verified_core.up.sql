CREATE SCHEMA verified;

-- UUID references only. No FK to identity or listings tables.
CREATE TABLE verified.appointments (
    id UUID PRIMARY KEY,
    listing_id UUID NOT NULL,
    requester_user_id UUID NOT NULL,
    provider_user_id UUID NOT NULL,
    status TEXT NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL,
    scheduled_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT appointments_participants_distinct CHECK (requester_user_id <> provider_user_id),
    CONSTRAINT appointments_status_allowed CHECK (
        status IN ('requested', 'accepted', 'rejected', 'cancelled', 'completed', 'no_show')
    )
);

CREATE TABLE verified.verification_challenges (
    id UUID PRIMARY KEY,
    appointment_id UUID NOT NULL REFERENCES verified.appointments (id),
    method TEXT NOT NULL,
    token_hash BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT verification_challenges_method_allowed CHECK (method IN ('otp', 'qr'))
);

CREATE TABLE verified.verified_interactions (
    id UUID PRIMARY KEY,
    appointment_id UUID NOT NULL UNIQUE REFERENCES verified.appointments (id),
    listing_id UUID NOT NULL,
    requester_user_id UUID NOT NULL,
    provider_user_id UUID NOT NULL,
    interaction_type TEXT NOT NULL,
    verification_method TEXT NOT NULL,
    verified_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT verified_interactions_type_allowed CHECK (interaction_type IN ('listing_inspection')),
    CONSTRAINT verified_interactions_method_allowed CHECK (verification_method IN ('otp', 'qr'))
);

CREATE INDEX appointments_requester_updated_idx
    ON verified.appointments (requester_user_id, updated_at DESC, id DESC);

CREATE INDEX appointments_provider_updated_idx
    ON verified.appointments (provider_user_id, updated_at DESC, id DESC);

CREATE INDEX verification_challenges_appointment_idx
    ON verified.verification_challenges (appointment_id, created_at DESC, id DESC);
