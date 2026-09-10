CREATE TABLE moderation.case_evidence (
    id UUID PRIMARY KEY,
    case_id UUID NOT NULL REFERENCES moderation.cases (id),
    evidence_type TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    reference_value TEXT,
    actor_staff_id UUID,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT moderation_case_evidence_type_allowed CHECK (evidence_type IN (
        'staff_note',
        'external_reference',
        'internal_reference',
        'snapshot_reference'
    )),
    CONSTRAINT moderation_case_evidence_title_bound CHECK (char_length(title) > 0 AND char_length(title) <= 200),
    CONSTRAINT moderation_case_evidence_description_bound CHECK (description IS NULL OR char_length(description) <= 2000),
    CONSTRAINT moderation_case_evidence_reference_bound CHECK (reference_value IS NULL OR (char_length(reference_value) > 0 AND char_length(reference_value) <= 512)),
    CONSTRAINT moderation_case_evidence_fields CHECK (
        (evidence_type = 'staff_note' AND reference_value IS NULL) OR
        (evidence_type IN ('external_reference', 'internal_reference', 'snapshot_reference') AND reference_value IS NOT NULL)
    )
);

CREATE INDEX moderation_case_evidence_case_created_idx
    ON moderation.case_evidence (case_id, created_at ASC, id ASC);

ALTER TABLE moderation.case_history DROP CONSTRAINT moderation_case_history_kind_allowed;

ALTER TABLE moderation.case_history ADD COLUMN evidence_id UUID REFERENCES moderation.case_evidence (id);

ALTER TABLE moderation.case_history ADD CONSTRAINT moderation_case_history_kind_allowed CHECK (kind IN (
    'case_created',
    'report_attached',
    'status_changed',
    'priority_changed',
    'assignment_changed',
    'staff_note_added',
    'evidence_added'
));

ALTER TABLE moderation.case_history ADD CONSTRAINT moderation_case_history_evidence_kind CHECK (
    (kind = 'evidence_added' AND evidence_id IS NOT NULL) OR
    (kind <> 'evidence_added' AND evidence_id IS NULL)
);
