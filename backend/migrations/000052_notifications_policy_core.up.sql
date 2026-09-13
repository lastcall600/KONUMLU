-- Additive NOTIFY-A policy tables. Does not alter notifications.deliveries
-- or notifications.warning_intents. No Identity FKs, ENUMs, or backfill.

CREATE TABLE notifications.preference_settings (
    user_id UUID NOT NULL,
    channel TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_key TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT notifications_preference_settings_pk
        PRIMARY KEY (user_id, channel, scope_type, scope_key),
    CONSTRAINT notifications_preference_settings_channel_check CHECK (
        channel IN ('in_app', 'web_push', 'mobile_push', 'email', 'sms')
    ),
    CONSTRAINT notifications_preference_settings_scope_type_check CHECK (
        scope_type IN ('channel', 'category', 'event')
    ),
    CONSTRAINT notifications_preference_settings_scope_key_check CHECK (
        (scope_type = 'channel' AND scope_key = '*')
        OR (
            scope_type = 'category'
            AND char_length(scope_key) BETWEEN 1 AND 64
            AND scope_key ~ '^[a-z][a-z0-9_]*$'
        )
        OR (
            scope_type = 'event'
            AND char_length(scope_key) BETWEEN 1 AND 128
            AND scope_key ~ '^[a-z][a-z0-9_.]*$'
        )
    ),
    CONSTRAINT notifications_preference_settings_updated_not_before_created
        CHECK (updated_at >= created_at)
);

CREATE TABLE notifications.consent_decisions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    consent_type TEXT NOT NULL,
    decision TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    source TEXT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,
    recorded_seq BIGINT GENERATED ALWAYS AS IDENTITY NOT NULL,
    CONSTRAINT notifications_consent_decisions_recorded_seq_unique UNIQUE (recorded_seq),
    CONSTRAINT notifications_consent_decisions_decision_check CHECK (
        decision IN ('granted', 'withdrawn')
    ),
    CONSTRAINT notifications_consent_decisions_consent_type_check CHECK (
        char_length(consent_type) BETWEEN 1 AND 128
        AND consent_type ~ '^[a-z][a-z0-9_.]*$'
    ),
    CONSTRAINT notifications_consent_decisions_source_check CHECK (
        char_length(source) BETWEEN 1 AND 64
        AND source ~ '^[a-z][a-z0-9_]*$'
    ),
    CONSTRAINT notifications_consent_decisions_policy_version_check CHECK (
        char_length(policy_version) BETWEEN 1 AND 64
    )
);

CREATE INDEX notifications_consent_decisions_user_type_seq_idx
    ON notifications.consent_decisions (user_id, consent_type, recorded_seq DESC);

CREATE TABLE notifications.intents (
    id UUID PRIMARY KEY,
    recipient_user_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    purpose TEXT NOT NULL,
    catalog_version INTEGER NOT NULL,
    template_key TEXT NOT NULL,
    urgency TEXT NOT NULL,
    locale_hint TEXT,
    domain_ref TEXT NOT NULL,
    actor_ref TEXT,
    resource_ref TEXT,
    dedupe_key TEXT NOT NULL,
    variables JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    CONSTRAINT notifications_intents_id_recipient_unique UNIQUE (id, recipient_user_id),
    CONSTRAINT notifications_intents_recipient_dedupe_unique UNIQUE (recipient_user_id, dedupe_key),
    CONSTRAINT notifications_intents_purpose_check CHECK (
        purpose IN ('security', 'transactional', 'social', 'product', 'marketing')
    ),
    CONSTRAINT notifications_intents_urgency_check CHECK (
        urgency IN ('critical', 'normal', 'low')
    ),
    CONSTRAINT notifications_intents_locale_hint_check CHECK (
        locale_hint IS NULL OR locale_hint IN ('tr', 'en', 'ru', 'ar')
    ),
    CONSTRAINT notifications_intents_event_type_nonempty CHECK (char_length(event_type) > 0),
    CONSTRAINT notifications_intents_catalog_version_positive CHECK (catalog_version > 0),
    CONSTRAINT notifications_intents_template_key_nonempty CHECK (char_length(template_key) > 0),
    CONSTRAINT notifications_intents_domain_ref_nonempty CHECK (char_length(domain_ref) > 0),
    CONSTRAINT notifications_intents_dedupe_key_nonempty CHECK (char_length(dedupe_key) > 0),
    CONSTRAINT notifications_intents_variables_object_check CHECK (jsonb_typeof(variables) = 'object'),
    CONSTRAINT notifications_intents_expires_after_created CHECK (
        expires_at IS NULL OR expires_at > created_at
    )
);

CREATE INDEX notifications_intents_recipient_created_id_idx
    ON notifications.intents (recipient_user_id, created_at DESC, id DESC);

CREATE TABLE notifications.channel_deliveries (
    id UUID PRIMARY KEY,
    intent_id UUID NOT NULL,
    channel TEXT NOT NULL,
    state TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ,
    suppression_reason TEXT,
    last_error_class TEXT,
    provider_ref TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    CONSTRAINT notifications_channel_deliveries_intent_fk
        FOREIGN KEY (intent_id)
        REFERENCES notifications.intents (id)
        ON DELETE NO ACTION,
    CONSTRAINT notifications_channel_deliveries_intent_channel_unique UNIQUE (intent_id, channel),
    CONSTRAINT notifications_channel_deliveries_channel_check CHECK (
        channel IN ('in_app', 'web_push', 'mobile_push', 'email', 'sms')
    ),
    CONSTRAINT notifications_channel_deliveries_state_check CHECK (
        state IN (
            'pending',
            'processing',
            'accepted',
            'retryable_failed',
            'permanently_failed',
            'suppressed'
        )
    ),
    CONSTRAINT notifications_channel_deliveries_attempts_nonneg CHECK (attempts >= 0),
    CONSTRAINT notifications_channel_deliveries_suppression_reason_check CHECK (
        suppression_reason IS NULL OR suppression_reason IN (
            'user_preference',
            'consent_missing',
            'consent_withdrawn',
            'channel_unavailable',
            'no_destination',
            'deduplicated',
            'policy_suppressed',
            'account_disabled',
            'unknown_event'
        )
    ),
    CONSTRAINT notifications_channel_deliveries_suppression_consistency CHECK (
        (state = 'suppressed' AND suppression_reason IS NOT NULL)
        OR (state <> 'suppressed' AND suppression_reason IS NULL)
    ),
    CONSTRAINT notifications_channel_deliveries_updated_not_before_created
        CHECK (updated_at >= created_at)
);

CREATE INDEX notifications_channel_deliveries_retry_next_attempt_idx
    ON notifications.channel_deliveries (next_attempt_at, id)
    WHERE state IN ('pending', 'retryable_failed');

CREATE TABLE notifications.inbox_items (
    id UUID PRIMARY KEY,
    intent_id UUID NOT NULL,
    user_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    read_at TIMESTAMPTZ,
    CONSTRAINT notifications_inbox_items_intent_unique UNIQUE (intent_id),
    CONSTRAINT notifications_inbox_items_intent_recipient_fk
        FOREIGN KEY (intent_id, user_id)
        REFERENCES notifications.intents (id, recipient_user_id)
        ON DELETE NO ACTION,
    CONSTRAINT notifications_inbox_items_read_not_before_created CHECK (
        read_at IS NULL OR read_at >= created_at
    )
);

CREATE INDEX notifications_inbox_items_user_created_id_idx
    ON notifications.inbox_items (user_id, created_at DESC, id DESC);

CREATE INDEX notifications_inbox_items_user_unread_created_id_idx
    ON notifications.inbox_items (user_id, created_at DESC, id DESC)
    WHERE read_at IS NULL;
