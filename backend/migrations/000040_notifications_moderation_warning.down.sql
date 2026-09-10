DROP TABLE IF EXISTS notifications.warning_intents;

ALTER TABLE notifications.deliveries
    DROP CONSTRAINT IF EXISTS notifications_deliveries_channel_check;
ALTER TABLE notifications.deliveries
    ADD CONSTRAINT notifications_deliveries_channel_check
    CHECK (channel IN ('email', 'sms'));
