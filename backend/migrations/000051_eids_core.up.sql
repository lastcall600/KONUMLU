CREATE SCHEMA eids;

-- Listing EİDS verification attempts. Property and vehicle are separate rows.
-- This is not person/e-Devlet identity. No TCKN or official payload columns.

CREATE TABLE eids.verifications (
    id UUID PRIMARY KEY,
    listing_id UUID NOT NULL,
    verification_type TEXT NOT NULL,
    status TEXT NOT NULL,
    provider_reference TEXT,
    failure_code TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    requested_at TIMESTAMPTZ,
    verified_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    CONSTRAINT eids_verifications_type_check CHECK (
        verification_type IN ('property', 'vehicle')
    ),
    CONSTRAINT eids_verifications_status_check CHECK (
        status IN ('pending', 'in_progress', 'verified', 'failed', 'unavailable', 'expired')
    ),
    CONSTRAINT eids_verifications_failure_code_check CHECK (
        failure_code IS NULL OR failure_code IN (
            'provider_unavailable',
            'verification_failed',
            'expired'
        )
    ),
    CONSTRAINT eids_verifications_updated_not_before_created CHECK (updated_at >= created_at),
    CONSTRAINT eids_verifications_verified_status CHECK (
        (status = 'verified' AND verified_at IS NOT NULL)
        OR (status <> 'verified' AND verified_at IS NULL)
    ),
    CONSTRAINT eids_verifications_failed_status CHECK (
        (status = 'failed' AND failed_at IS NOT NULL)
        OR (status <> 'failed')
    ),
    CONSTRAINT eids_verifications_provider_reference_not_empty CHECK (
        provider_reference IS NULL OR btrim(provider_reference) <> ''
    )
);

CREATE INDEX eids_verifications_listing_type_updated_idx
    ON eids.verifications (listing_id, verification_type, updated_at DESC);

-- One open attempt per listing+type. Terminal rows may be followed by a new attempt.
CREATE UNIQUE INDEX eids_verifications_one_open
    ON eids.verifications (listing_id, verification_type)
    WHERE status IN ('pending', 'in_progress', 'unavailable');
