CREATE SCHEMA moderation;

-- UUID references only. No FK to identity or listings tables.
CREATE TABLE moderation.reports (
    id UUID PRIMARY KEY,
    reporter_user_id UUID NOT NULL,
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    reason_code TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT moderation_reports_target_type_allowed CHECK (target_type IN ('listing', 'public_profile')),
    CONSTRAINT moderation_reports_reason_code_allowed CHECK (reason_code IN (
        'spam',
        'scam_or_fraud',
        'prohibited_item',
        'harassment',
        'impersonation',
        'inappropriate_content',
        'other'
    )),
    CONSTRAINT moderation_reports_status_allowed CHECK (status IN ('submitted', 'triaged', 'closed')),
    CONSTRAINT moderation_reports_description_bound CHECK (description IS NULL OR char_length(description) <= 2000)
);

CREATE INDEX moderation_reports_reporter_created_idx
    ON moderation.reports (reporter_user_id, created_at DESC, id DESC);

CREATE UNIQUE INDEX moderation_reports_open_identical_idx
    ON moderation.reports (reporter_user_id, target_type, target_id, reason_code)
    WHERE status IN ('submitted', 'triaged');
