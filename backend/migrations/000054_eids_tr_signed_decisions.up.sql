-- Additive TR signed-decision mapping and replay/idempotency.
-- Does not alter eids.verifications. No ENUM. No TCKN/identity/provider payload columns.

CREATE TABLE eids.subject_refs (
    subject_ref TEXT PRIMARY KEY,
    verification_id UUID NOT NULL,
    listing_id UUID NOT NULL,
    verification_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT eids_subject_refs_ref_hex CHECK (subject_ref ~ '^[0-9a-f]{64}$'),
    CONSTRAINT eids_subject_refs_type_check CHECK (
        verification_type IN ('property', 'vehicle')
    )
);

CREATE UNIQUE INDEX eids_subject_refs_verification_uidx
    ON eids.subject_refs (verification_id);

CREATE INDEX eids_subject_refs_listing_type_idx
    ON eids.subject_refs (listing_id, verification_type);

CREATE TABLE eids.tr_signed_decisions (
    decision_id TEXT PRIMARY KEY,
    claims_hash BYTEA NOT NULL,
    subject_ref TEXT NOT NULL,
    verification_type TEXT NOT NULL,
    status TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    valid_until TIMESTAMPTZ NOT NULL,
    key_id TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT eids_tr_signed_decisions_hash_len CHECK (octet_length(claims_hash) = 32),
    CONSTRAINT eids_tr_signed_decisions_decision_id_len CHECK (
        char_length(decision_id) BETWEEN 16 AND 128
    ),
    CONSTRAINT eids_tr_signed_decisions_subject_hex CHECK (subject_ref ~ '^[0-9a-f]{64}$'),
    CONSTRAINT eids_tr_signed_decisions_type_check CHECK (
        verification_type IN ('property', 'vehicle')
    ),
    CONSTRAINT eids_tr_signed_decisions_status_check CHECK (
        status IN ('approved', 'rejected')
    ),
    CONSTRAINT eids_tr_signed_decisions_key_id_len CHECK (
        char_length(key_id) BETWEEN 1 AND 64
    ),
    CONSTRAINT eids_tr_signed_decisions_valid_until_after_issued CHECK (valid_until > issued_at),
    CONSTRAINT eids_tr_signed_decisions_applied_not_before_received CHECK (applied_at >= received_at)
);

CREATE INDEX eids_tr_signed_decisions_subject_idx
    ON eids.tr_signed_decisions (subject_ref);
