-- LOCAL DEVELOPMENT DATA ONLY. Deterministic restore-drill markers.
-- Do not run against production. Does not contain credentials.

BEGIN;

INSERT INTO identity.users (id, created_at, updated_at, disabled_at, deleted_at, session_epoch)
VALUES (
    'a1000001-0000-4000-8000-000000000001'::uuid,
    TIMESTAMPTZ '2026-09-12 00:00:00+00',
    TIMESTAMPTZ '2026-09-12 00:00:00+00',
    NULL,
    NULL,
    0
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO identity.public_profiles (
    user_id, public_profile_id, display_name, created_at, updated_at, moderation_state
) VALUES (
    'a1000001-0000-4000-8000-000000000001'::uuid,
    'a1000001-0000-4000-8000-000000000011'::uuid,
    'Restore Drill Profile',
    TIMESTAMPTZ '2026-09-12 00:00:00+00',
    TIMESTAMPTZ '2026-09-12 00:00:00+00',
    'none'
)
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO listings.listings (
    id, owner_user_id, status, category_id, category_schema_version,
    title, description, price_amount, price_currency, attributes,
    created_at, updated_at, published_at, archived_at, moderation_state
) VALUES (
    'a1000002-0000-4000-8000-000000000001'::uuid,
    'a1000001-0000-4000-8000-000000000001'::uuid,
    'published',
    '0b500001-0000-4000-a000-000000000001'::uuid,
    1,
    'RESTORE-DRILL-LISTING',
    'Local backup restore drill listing. Not production data.',
    1500.00,
    'TRY',
    '{}'::jsonb,
    TIMESTAMPTZ '2026-09-12 00:00:00+00',
    TIMESTAMPTZ '2026-09-12 00:01:00+00',
    TIMESTAMPTZ '2026-09-12 00:01:00+00',
    NULL,
    'none'
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO location.listing_locations (
    listing_id, catalog_location_id, point, created_at, updated_at
) VALUES (
    'a1000002-0000-4000-8000-000000000001'::uuid,
    NULL,
    ST_SetSRID(ST_MakePoint(32.8597, 39.9334), 4326)::geography,
    TIMESTAMPTZ '2026-09-12 00:01:00+00',
    TIMESTAMPTZ '2026-09-12 00:01:00+00'
)
ON CONFLICT (listing_id) DO NOTHING;

INSERT INTO moderation.reports (
    id, reporter_user_id, target_type, target_id, reason_code, description,
    status, created_at, updated_at
) VALUES (
    'a1000003-0000-4000-8000-000000000002'::uuid,
    'a1000001-0000-4000-8000-000000000001'::uuid,
    'listing',
    'a1000002-0000-4000-8000-000000000001'::uuid,
    'spam',
    'RESTORE-DRILL-REPORT',
    'triaged',
    TIMESTAMPTZ '2026-09-12 00:02:00+00',
    TIMESTAMPTZ '2026-09-12 00:02:00+00'
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO moderation.cases (
    id, status, priority, subject_type, subject_id, title,
    assigned_staff_id, created_at, updated_at
) VALUES (
    'a1000003-0000-4000-8000-000000000001'::uuid,
    'open',
    'normal',
    'listing',
    'a1000002-0000-4000-8000-000000000001'::uuid,
    'RESTORE-DRILL-CASE',
    NULL,
    TIMESTAMPTZ '2026-09-12 00:03:00+00',
    TIMESTAMPTZ '2026-09-12 00:03:00+00'
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO moderation.case_reports (case_id, report_id, attached_at)
VALUES (
    'a1000003-0000-4000-8000-000000000001'::uuid,
    'a1000003-0000-4000-8000-000000000002'::uuid,
    TIMESTAMPTZ '2026-09-12 00:03:00+00'
)
ON CONFLICT (case_id, report_id) DO NOTHING;

INSERT INTO moderation.case_history (
    id, case_id, kind, actor_staff_id, report_id,
    from_status, to_status, from_priority, to_priority,
    from_assigned_staff_id, to_assigned_staff_id, note, created_at
)
SELECT
    'a1000003-0000-4000-8000-000000000003'::uuid,
    'a1000003-0000-4000-8000-000000000001'::uuid,
    'case_created',
    NULL,
    NULL,
    NULL,
    'open',
    NULL,
    'normal',
    NULL,
    NULL,
    'RESTORE-DRILL-HISTORY',
    TIMESTAMPTZ '2026-09-12 00:03:00+00'
WHERE NOT EXISTS (
    SELECT 1 FROM moderation.case_history
    WHERE id = 'a1000003-0000-4000-8000-000000000003'::uuid
);

COMMIT;
