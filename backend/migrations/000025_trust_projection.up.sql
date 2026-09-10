CREATE SCHEMA trust;

-- Derived, rebuildable trust projections. Canonical events remain in source domains.
-- Trust is not the source of truth for appointments, verified interactions,
-- identity verification, listings, or transactions.

CREATE TABLE trust.processed_events (
    event_id UUID PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE trust.user_profiles (
    user_id UUID PRIMARY KEY,
    verified_interaction_count INTEGER NOT NULL,
    provider_verified_interaction_count INTEGER NOT NULL,
    requester_verified_interaction_count INTEGER NOT NULL,
    last_verified_interaction_at TIMESTAMPTZ,
    trust_level TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT user_profiles_counts_nonnegative CHECK (
        verified_interaction_count >= 0
        AND provider_verified_interaction_count >= 0
        AND requester_verified_interaction_count >= 0
    ),
    CONSTRAINT user_profiles_count_sum CHECK (
        verified_interaction_count = provider_verified_interaction_count + requester_verified_interaction_count
    ),
    CONSTRAINT user_profiles_trust_level_allowed CHECK (
        trust_level IN ('new', 'verified', 'established')
    )
);

-- Public-safe verified-interaction history for Güven Pasaportu. No listing private data.
CREATE TABLE trust.user_verified_history (
    interaction_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role TEXT NOT NULL,
    listing_id UUID NOT NULL,
    interaction_type TEXT NOT NULL,
    verification_method TEXT NOT NULL,
    verified_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (interaction_id, user_id),
    CONSTRAINT user_verified_history_role_allowed CHECK (role IN ('requester', 'provider'))
);

CREATE INDEX user_profiles_updated_idx
    ON trust.user_profiles (updated_at DESC, user_id);

CREATE INDEX user_verified_history_user_verified_idx
    ON trust.user_verified_history (user_id, verified_at DESC, interaction_id DESC);
