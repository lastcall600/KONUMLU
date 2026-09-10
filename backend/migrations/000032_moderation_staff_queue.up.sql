ALTER TABLE moderation.reports
    ADD COLUMN staff_note TEXT,
    ADD COLUMN status_changed_by UUID;

ALTER TABLE moderation.reports
    ADD CONSTRAINT moderation_reports_staff_note_bound
        CHECK (staff_note IS NULL OR char_length(staff_note) <= 2000);

CREATE INDEX moderation_reports_queue_created_idx
    ON moderation.reports (created_at DESC, id DESC);

CREATE INDEX moderation_reports_queue_status_created_idx
    ON moderation.reports (status, created_at DESC, id DESC);
