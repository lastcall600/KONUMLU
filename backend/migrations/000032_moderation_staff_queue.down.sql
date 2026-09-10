DROP INDEX IF EXISTS moderation.moderation_reports_queue_status_created_idx;
DROP INDEX IF EXISTS moderation.moderation_reports_queue_created_idx;

ALTER TABLE moderation.reports
    DROP CONSTRAINT IF EXISTS moderation_reports_staff_note_bound;

ALTER TABLE moderation.reports
    DROP COLUMN IF EXISTS status_changed_by,
    DROP COLUMN IF EXISTS staff_note;
