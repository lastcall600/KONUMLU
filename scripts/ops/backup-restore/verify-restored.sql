-- Objective restore verification. Exits non-zero via RAISE EXCEPTION on any miss.
-- Expects the local drill dataset from seed-local-drill.sql.

DO $$
DECLARE
    v_postgis boolean;
    v_postgis_ver text;
    v_mig bigint;
    v_dirty boolean;
    v_listing_count bigint;
    v_case_count bigint;
    v_profile_count bigint;
    v_lat double precision;
    v_lon double precision;
    v_fk bigint;
BEGIN
    SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'postgis') INTO v_postgis;
    IF NOT v_postgis THEN
        RAISE EXCEPTION 'verify failed: postgis extension missing';
    END IF;

    SELECT PostGIS_Version() INTO v_postgis_ver;
    IF v_postgis_ver IS NULL OR length(v_postgis_ver) = 0 THEN
        RAISE EXCEPTION 'verify failed: PostGIS_Version() empty';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'schema_migrations'
    ) THEN
        RAISE EXCEPTION 'verify failed: schema_migrations missing (empty or unrestored database)';
    END IF;

    SELECT version, dirty INTO v_mig, v_dirty FROM schema_migrations;
    IF v_dirty THEN
        RAISE EXCEPTION 'verify failed: dirty migration version %', v_mig;
    END IF;
    IF v_mig IS DISTINCT FROM 51 THEN
        RAISE EXCEPTION 'verify failed: expected migration version 51, got %', v_mig;
    END IF;

    IF to_regclass('identity.users') IS NULL
        OR to_regclass('listings.listings') IS NULL
        OR to_regclass('moderation.cases') IS NULL
        OR to_regclass('location.listing_locations') IS NULL THEN
        RAISE EXCEPTION 'verify failed: expected domain tables missing';
    END IF;

    SELECT COUNT(*) INTO v_profile_count
    FROM identity.public_profiles
    WHERE public_profile_id = 'a1000001-0000-4000-8000-000000000011'::uuid
      AND user_id = 'a1000001-0000-4000-8000-000000000001'::uuid
      AND display_name = 'Restore Drill Profile';
    IF v_profile_count <> 1 THEN
        RAISE EXCEPTION 'verify failed: drill public profile missing';
    END IF;

    SELECT COUNT(*) INTO v_listing_count
    FROM listings.listings
    WHERE id = 'a1000002-0000-4000-8000-000000000001'::uuid
      AND owner_user_id = 'a1000001-0000-4000-8000-000000000001'::uuid
      AND status = 'published'
      AND title = 'RESTORE-DRILL-LISTING';
    IF v_listing_count <> 1 THEN
        RAISE EXCEPTION 'verify failed: drill listing missing';
    END IF;

    SELECT ST_Y(point::geometry), ST_X(point::geometry)
    INTO v_lat, v_lon
    FROM location.listing_locations
    WHERE listing_id = 'a1000002-0000-4000-8000-000000000001'::uuid;
    IF v_lat IS NULL OR abs(v_lat - 39.9334) > 0.0001 OR abs(v_lon - 32.8597) > 0.0001 THEN
        RAISE EXCEPTION 'verify failed: PostGIS drill point mismatch';
    END IF;

    SELECT COUNT(*) INTO v_case_count
    FROM moderation.cases
    WHERE id = 'a1000003-0000-4000-8000-000000000001'::uuid
      AND title = 'RESTORE-DRILL-CASE'
      AND subject_id = 'a1000002-0000-4000-8000-000000000001'::uuid;
    IF v_case_count <> 1 THEN
        RAISE EXCEPTION 'verify failed: drill moderation case missing';
    END IF;

    SELECT COUNT(*) INTO v_fk
    FROM moderation.case_reports cr
    JOIN moderation.reports r ON r.id = cr.report_id
    JOIN moderation.cases c ON c.id = cr.case_id
    JOIN moderation.case_history h ON h.case_id = c.id
    WHERE c.id = 'a1000003-0000-4000-8000-000000000001'::uuid
      AND r.id = 'a1000003-0000-4000-8000-000000000002'::uuid
      AND h.id = 'a1000003-0000-4000-8000-000000000003'::uuid;
    IF v_fk <> 1 THEN
        RAISE EXCEPTION 'verify failed: drill case/report/history relationship missing';
    END IF;

    RAISE NOTICE 'RESTORE VERIFY PASS migration=% postgis=%', v_mig, v_postgis_ver;
END $$;
