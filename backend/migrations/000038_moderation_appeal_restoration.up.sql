ALTER TABLE moderation.case_history DROP CONSTRAINT moderation_case_history_kind_allowed;
ALTER TABLE moderation.case_history DROP CONSTRAINT moderation_case_history_appeal_kind;
ALTER TABLE moderation.case_history DROP CONSTRAINT moderation_case_history_action_kind;

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
    'action_cancelled',
    'appeal_submitted',
    'appeal_review_started',
    'appeal_accepted',
    'appeal_rejected',
    'appeal_withdrawn',
    'appeal_restoration_applied'
));

ALTER TABLE moderation.case_history ADD CONSTRAINT moderation_case_history_appeal_kind CHECK (
    (kind IN ('appeal_submitted', 'appeal_review_started', 'appeal_accepted', 'appeal_rejected', 'appeal_withdrawn', 'appeal_restoration_applied') AND appeal_id IS NOT NULL) OR
    (kind NOT IN ('appeal_submitted', 'appeal_review_started', 'appeal_accepted', 'appeal_rejected', 'appeal_withdrawn', 'appeal_restoration_applied') AND appeal_id IS NULL)
);

ALTER TABLE moderation.case_history ADD CONSTRAINT moderation_case_history_action_kind CHECK (
    (kind IN ('action_proposed', 'action_approved', 'action_executed', 'action_cancelled', 'appeal_restoration_applied') AND action_id IS NOT NULL) OR
    (kind NOT IN ('action_proposed', 'action_approved', 'action_executed', 'action_cancelled', 'appeal_restoration_applied') AND action_id IS NULL)
);
