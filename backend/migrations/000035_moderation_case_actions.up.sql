CREATE TABLE moderation.case_actions (
    id UUID PRIMARY KEY,
    case_id UUID NOT NULL REFERENCES moderation.cases (id),
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    action_type TEXT NOT NULL,
    status TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    rationale TEXT,
    actor_staff_id UUID,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT moderation_case_actions_target_type_allowed CHECK (target_type IN ('listing', 'public_profile')),
    CONSTRAINT moderation_case_actions_type_allowed CHECK (action_type IN (
        'no_action',
        'warning',
        'restrict',
        'suspend',
        'remove'
    )),
    CONSTRAINT moderation_case_actions_status_allowed CHECK (status IN (
        'proposed',
        'approved',
        'executed',
        'cancelled'
    )),
    CONSTRAINT moderation_case_actions_reason_allowed CHECK (reason_code IN (
        'no_violation',
        'policy_violation',
        'repeated_violation',
        'safety_risk',
        'prohibited_content',
        'other'
    )),
    CONSTRAINT moderation_case_actions_rationale_bound CHECK (rationale IS NULL OR char_length(rationale) <= 2000)
);

CREATE INDEX moderation_case_actions_case_created_idx
    ON moderation.case_actions (case_id, created_at ASC, id ASC);

CREATE INDEX moderation_case_actions_status_created_idx
    ON moderation.case_actions (status, created_at DESC, id DESC);

CREATE INDEX moderation_case_actions_case_status_created_idx
    ON moderation.case_actions (case_id, status, created_at ASC, id ASC);

ALTER TABLE moderation.case_history DROP CONSTRAINT moderation_case_history_kind_allowed;

ALTER TABLE moderation.case_history ADD COLUMN action_id UUID REFERENCES moderation.case_actions (id);

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

ALTER TABLE moderation.case_history ADD CONSTRAINT moderation_case_history_action_kind CHECK (
    (kind IN ('action_proposed', 'action_approved', 'action_executed', 'action_cancelled') AND action_id IS NOT NULL) OR
    (kind NOT IN ('action_proposed', 'action_approved', 'action_executed', 'action_cancelled') AND action_id IS NULL)
);
