CREATE SCHEMA disputes;

CREATE TABLE disputes.disputes (
    id UUID PRIMARY KEY,
    transaction_id UUID NOT NULL,
    requester_user_id UUID NOT NULL,
    provider_user_id UUID NOT NULL,
    opened_by_user_id UUID NOT NULL,
    reason_code TEXT NOT NULL,
    statement TEXT,
    status TEXT NOT NULL,
    resolution_code TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    CONSTRAINT disputes_disputes_reason_check CHECK (
        reason_code IN (
            'item_or_service_not_as_described',
            'non_delivery',
            'damaged_or_incomplete',
            'payment_issue',
            'cancellation_issue',
            'other'
        )
    ),
    CONSTRAINT disputes_disputes_status_check CHECK (
        status IN ('open', 'under_review', 'resolved', 'closed', 'withdrawn')
    ),
    CONSTRAINT disputes_disputes_resolution_check CHECK (
        resolution_code IS NULL OR resolution_code IN (
            'no_action',
            'buyer_favored',
            'provider_favored',
            'mutual_resolution',
            'insufficient_evidence',
            'other'
        )
    ),
    CONSTRAINT disputes_disputes_statement_len_check CHECK (
        statement IS NULL OR char_length(statement) BETWEEN 1 AND 2000
    ),
    CONSTRAINT disputes_disputes_updated_not_before_created CHECK (updated_at >= created_at),
    CONSTRAINT disputes_disputes_distinct_parties CHECK (requester_user_id <> provider_user_id),
    CONSTRAINT disputes_disputes_opener_is_party CHECK (
        opened_by_user_id = requester_user_id OR opened_by_user_id = provider_user_id
    ),
    CONSTRAINT disputes_disputes_status_resolution_check CHECK (
        (status IN ('open', 'under_review', 'withdrawn')
            AND resolved_at IS NULL
            AND resolution_code IS NULL)
        OR (status IN ('resolved', 'closed')
            AND resolved_at IS NOT NULL
            AND resolution_code IS NOT NULL
            AND resolved_at >= created_at)
    )
);

CREATE UNIQUE INDEX disputes_disputes_one_active_per_transaction_idx
    ON disputes.disputes (transaction_id)
    WHERE status IN ('open', 'under_review');

CREATE INDEX disputes_disputes_transaction_created_id_idx
    ON disputes.disputes (transaction_id, created_at DESC, id);

CREATE INDEX disputes_disputes_requester_created_id_idx
    ON disputes.disputes (requester_user_id, created_at DESC, id);

CREATE INDEX disputes_disputes_provider_created_id_idx
    ON disputes.disputes (provider_user_id, created_at DESC, id);

CREATE INDEX disputes_disputes_status_created_id_idx
    ON disputes.disputes (status, created_at DESC, id);

CREATE TABLE disputes.evidence (
    id UUID PRIMARY KEY,
    dispute_id UUID NOT NULL REFERENCES disputes.disputes (id),
    evidence_type TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    reference_value TEXT,
    actor_user_id UUID,
    actor_role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT disputes_evidence_type_check CHECK (
        evidence_type IN ('party_statement', 'external_reference', 'internal_reference')
    ),
    CONSTRAINT disputes_evidence_title_bound CHECK (char_length(title) BETWEEN 1 AND 200),
    CONSTRAINT disputes_evidence_description_bound CHECK (
        description IS NULL OR char_length(description) BETWEEN 1 AND 2000
    ),
    CONSTRAINT disputes_evidence_reference_bound CHECK (
        reference_value IS NULL OR char_length(reference_value) BETWEEN 1 AND 512
    ),
    CONSTRAINT disputes_evidence_role_check CHECK (
        actor_role IN ('requester', 'provider', 'internal')
    ),
    CONSTRAINT disputes_evidence_fields_check CHECK (
        (evidence_type = 'party_statement' AND reference_value IS NULL)
        OR (evidence_type IN ('external_reference', 'internal_reference') AND reference_value IS NOT NULL)
    )
);

CREATE INDEX disputes_evidence_dispute_created_id_idx
    ON disputes.evidence (dispute_id, created_at ASC, id ASC);
