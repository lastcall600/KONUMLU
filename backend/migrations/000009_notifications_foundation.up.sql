CREATE SCHEMA IF NOT EXISTS notifications;

CREATE TABLE notifications.deliveries (
    id UUID PRIMARY KEY,
    intent_id UUID NOT NULL,
    channel TEXT NOT NULL,
    template_code TEXT NOT NULL,
    template_version INTEGER NOT NULL,
    locale TEXT NOT NULL,
    recipient_kind TEXT NOT NULL,
    recipient_id UUID NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    provider_ref TEXT,
    correlation_id TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    last_attempt_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT notifications_deliveries_channel_check CHECK (channel IN ('email', 'sms')),
    CONSTRAINT notifications_deliveries_locale_check CHECK (locale IN ('tr', 'en', 'ru', 'ar')),
    CONSTRAINT notifications_deliveries_recipient_kind_check CHECK (recipient_kind IN ('verification_challenge', 'user')),
    CONSTRAINT notifications_deliveries_status_check CHECK (status IN ('pending', 'sending', 'sent', 'failed')),
    CONSTRAINT notifications_deliveries_template_code_nonempty CHECK (char_length(template_code) > 0),
    CONSTRAINT notifications_deliveries_template_version_positive CHECK (template_version > 0),
    CONSTRAINT notifications_deliveries_attempts_nonneg CHECK (attempts >= 0),
    CONSTRAINT notifications_deliveries_updated_not_before_created CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX notifications_deliveries_intent_id_uidx
    ON notifications.deliveries (intent_id);
