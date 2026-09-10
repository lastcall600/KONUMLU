ALTER TABLE moderation.case_history DROP CONSTRAINT IF EXISTS moderation_case_history_action_kind;
ALTER TABLE moderation.case_history DROP CONSTRAINT IF EXISTS moderation_case_history_kind_allowed;
ALTER TABLE moderation.case_history DROP COLUMN IF EXISTS action_id;

ALTER TABLE moderation.case_history ADD CONSTRAINT moderation_case_history_kind_allowed CHECK (kind IN (
    'case_created',
    'report_attached',
    'status_changed',
    'priority_changed',
    'assignment_changed',
    'staff_note_added',
    'evidence_added'
));

DROP INDEX IF EXISTS moderation.moderation_case_actions_case_status_created_idx;
DROP INDEX IF EXISTS moderation.moderation_case_actions_status_created_idx;
DROP INDEX IF EXISTS moderation.moderation_case_actions_case_created_idx;
DROP TABLE IF EXISTS moderation.case_actions;
