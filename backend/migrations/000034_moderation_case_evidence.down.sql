ALTER TABLE moderation.case_history DROP CONSTRAINT IF EXISTS moderation_case_history_evidence_kind;
ALTER TABLE moderation.case_history DROP CONSTRAINT IF EXISTS moderation_case_history_kind_allowed;
ALTER TABLE moderation.case_history DROP COLUMN IF EXISTS evidence_id;

ALTER TABLE moderation.case_history ADD CONSTRAINT moderation_case_history_kind_allowed CHECK (kind IN (
    'case_created',
    'report_attached',
    'status_changed',
    'priority_changed',
    'assignment_changed',
    'staff_note_added'
));

DROP INDEX IF EXISTS moderation.moderation_case_evidence_case_created_idx;
DROP TABLE IF EXISTS moderation.case_evidence;
