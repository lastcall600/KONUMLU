CREATE TABLE moderation.appeals (
    id UUID PRIMARY KEY,
    action_id UUID NOT NULL REFERENCES moderation.case_actions (id),
    case_id UUID NOT NULL REFERENCES moderation.cases (id),
    appellant_user_id UUID NOT NULL,
    statement TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    decided_at TIMESTAMPTZ,
    decided_by_staff_id UUID,
    CONSTRAINT moderation_appeals_status_allowed CHECK (status IN (
        'submitted',
        'under_review',
        'accepted',
        'rejected',
        'withdrawn'
    )),
    CONSTRAINT moderation_appeals_statement_bound CHECK (char_length(statement) > 0 AND char_length(statement) <= 2000),
    CONSTRAINT moderation_appeals_decided_at_terminal CHECK (
        (status IN ('accepted', 'rejected', 'withdrawn') AND decided_at IS NOT NULL) OR
        (status IN ('submitted', 'under_review') AND decided_at IS NULL)
    ),
    CONSTRAINT moderation_appeals_decided_by_staff CHECK (
        (status IN ('accepted', 'rejected') AND TRUE) OR
        (status NOT IN ('accepted', 'rejected') AND decided_by_staff_id IS NULL)
    )
);

CREATE UNIQUE INDEX moderation_appeals_one_active_uidx
    ON moderation.appeals (action_id, appellant_user_id)
    WHERE status IN ('submitted', 'under_review');

CREATE INDEX moderation_appeals_action_status_created_idx
    ON moderation.appeals (action_id, status, created_at DESC, id DESC);

CREATE INDEX moderation_appeals_appellant_created_idx
    ON moderation.appeals (appellant_user_id, created_at DESC, id DESC);

CREATE INDEX moderation_appeals_status_created_idx
    ON moderation.appeals (status, created_at DESC, id DESC);

CREATE INDEX moderation_appeals_case_created_idx
    ON moderation.appeals (case_id, created_at ASC, id ASC);

ALTER TABLE moderation.case_history DROP CONSTRAINT moderation_case_history_kind_allowed;

ALTER TABLE moderation.case_history ADD COLUMN appeal_id UUID REFERENCES moderation.appeals (id);

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
    'appeal_withdrawn'
));

ALTER TABLE moderation.case_history ADD CONSTRAINT moderation_case_history_appeal_kind CHECK (
    (kind IN ('appeal_submitted', 'appeal_review_started', 'appeal_accepted', 'appeal_rejected', 'appeal_withdrawn') AND appeal_id IS NOT NULL) OR
    (kind NOT IN ('appeal_submitted', 'appeal_review_started', 'appeal_accepted', 'appeal_rejected', 'appeal_withdrawn') AND appeal_id IS NULL)
);
