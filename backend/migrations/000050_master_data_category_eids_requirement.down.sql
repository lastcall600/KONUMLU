ALTER TABLE master_data.categories
    DROP CONSTRAINT IF EXISTS master_data_categories_eids_requirement_check;

ALTER TABLE master_data.categories
    DROP COLUMN IF EXISTS eids_requirement;
