-- DEVELOPMENT SEED ONLY
-- Removes only the fixed UUID rows inserted by 000019_dev_master_data_seed.up.sql.
-- Order: schemas first (cascades attributes/options/labels), then categories (cascades category labels).

DELETE FROM master_data.category_schemas
WHERE id IN (
    '0b500001-0000-4000-a000-000000000011'::uuid,
    '0b500001-0000-4000-a000-000000000012'::uuid,
    '0b500001-0000-4000-a000-000000000013'::uuid
);

DELETE FROM master_data.categories
WHERE id IN (
    '0b500001-0000-4000-a000-000000000001'::uuid,
    '0b500001-0000-4000-a000-000000000002'::uuid,
    '0b500001-0000-4000-a000-000000000003'::uuid
);
