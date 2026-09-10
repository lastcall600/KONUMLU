ALTER TABLE moderation.case_history DROP CONSTRAINT IF EXISTS moderation_case_history_appeal_kind;
ALTER TABLE moderation.case_history DROP CONSTRAINT IF EXISTS moderation_case_history_kind_allowed;
ALTER TABLE moderation.case_history DROP COLUMN IF EXISTS appeal_id;

ALTER TABLE moderation.case_history ADD CONSTRAINT moderation_case_history_kind_allowed CHECK (kind IN (
    'case_created',
    'report_attached',
    'status_changed',
    'priority_changed',
    'assignment_changed',
    'staff_note_added',
    'evidence_added',
    'action_proposed',
    'action_approved',
    'action_executed',
    'action_cancelled'
));

DROP INDEX IF EXISTS moderation.moderation_appeals_case_created_idx;
DROP INDEX IF EXISTS moderation.moderation_appeals_status_created_idx;
DROP INDEX IF EXISTS moderation.moderation_appeals_appellant_created_idx;
DROP INDEX IF EXISTS moderation.moderation_appeals_action_status_created_idx;
DROP INDEX IF EXISTS moderation.moderation_appeals_one_active_uidx;
DROP TABLE IF EXISTS moderation.appeals;
