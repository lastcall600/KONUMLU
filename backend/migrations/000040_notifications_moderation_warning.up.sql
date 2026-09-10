-- In-app moderation warning intents. Recipient lives on notifications.deliveries.

ALTER TABLE notifications.deliveries
    DROP CONSTRAINT IF EXISTS notifications_deliveries_channel_check;
ALTER TABLE notifications.deliveries
    ADD CONSTRAINT notifications_deliveries_channel_check
    CHECK (channel IN ('email', 'sms', 'in_app'));

CREATE TABLE notifications.warning_intents (
    intent_id UUID PRIMARY KEY,
    locale TEXT NOT NULL,
    template_code TEXT NOT NULL,
    message_key TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_ref UUID NOT NULL,
    reason_code TEXT NOT NULL,
    action_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT notifications_warning_intents_locale_check CHECK (locale IN ('tr', 'en', 'ru', 'ar')),
    CONSTRAINT notifications_warning_intents_target_type_check CHECK (target_type IN ('listing', 'public_profile')),
    CONSTRAINT notifications_warning_intents_template_code_nonempty CHECK (char_length(template_code) > 0),
    CONSTRAINT notifications_warning_intents_message_key_nonempty CHECK (char_length(message_key) > 0),
    CONSTRAINT notifications_warning_intents_reason_code_nonempty CHECK (char_length(reason_code) > 0),
    CONSTRAINT notifications_warning_intents_updated_not_before_created CHECK (updated_at >= created_at)
);

CREATE INDEX notifications_warning_intents_target_idx
    ON notifications.warning_intents (target_type, target_ref);
