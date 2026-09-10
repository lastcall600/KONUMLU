CREATE TABLE moderation.cases (
    id UUID PRIMARY KEY,
    status TEXT NOT NULL,
    priority TEXT NOT NULL,
    subject_type TEXT NOT NULL,
    subject_id UUID NOT NULL,
    title TEXT NOT NULL,
    assigned_staff_id UUID,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT moderation_cases_status_allowed CHECK (status IN ('open', 'investigating', 'resolved', 'closed')),
    CONSTRAINT moderation_cases_priority_allowed CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    CONSTRAINT moderation_cases_subject_type_allowed CHECK (subject_type IN ('listing', 'public_profile')),
    CONSTRAINT moderation_cases_title_bound CHECK (char_length(title) > 0 AND char_length(title) <= 200)
);

CREATE TABLE moderation.case_reports (
    case_id UUID NOT NULL REFERENCES moderation.cases (id),
    report_id UUID NOT NULL REFERENCES moderation.reports (id),
    attached_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (case_id, report_id)
);

CREATE UNIQUE INDEX moderation_case_reports_report_id_uidx
    ON moderation.case_reports (report_id);

CREATE TABLE moderation.case_history (
    id UUID PRIMARY KEY,
    case_id UUID NOT NULL REFERENCES moderation.cases (id),
    kind TEXT NOT NULL,
    actor_staff_id UUID,
    report_id UUID,
    from_status TEXT,
    to_status TEXT,
    from_priority TEXT,
    to_priority TEXT,
    from_assigned_staff_id UUID,
    to_assigned_staff_id UUID,
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT moderation_case_history_kind_allowed CHECK (kind IN (
        'case_created',
        'report_attached',
        'status_changed',
        'priority_changed',
        'assignment_changed',
        'staff_note_added'
    )),
    CONSTRAINT moderation_case_history_note_bound CHECK (note IS NULL OR char_length(note) <= 2000)
);

CREATE INDEX moderation_cases_created_idx
    ON moderation.cases (created_at DESC, id DESC);

CREATE INDEX moderation_cases_status_created_idx
    ON moderation.cases (status, created_at DESC, id DESC);

CREATE INDEX moderation_cases_priority_created_idx
    ON moderation.cases (priority, created_at DESC, id DESC);

CREATE INDEX moderation_cases_subject_type_created_idx
    ON moderation.cases (subject_type, created_at DESC, id DESC);

CREATE INDEX moderation_case_reports_case_attached_idx
    ON moderation.case_reports (case_id, attached_at ASC, report_id ASC);

CREATE INDEX moderation_case_history_case_created_idx
    ON moderation.case_history (case_id, created_at ASC, id ASC);
