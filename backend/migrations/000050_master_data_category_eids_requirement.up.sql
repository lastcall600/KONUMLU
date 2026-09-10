-- Server-side EİDS listing requirement per category.
-- none | property | vehicle are separate. This is not person/e-Devlet identity.
-- Development categories remain none until official production mappings exist.

ALTER TABLE master_data.categories
    ADD COLUMN eids_requirement TEXT NOT NULL DEFAULT 'none';

ALTER TABLE master_data.categories
    ADD CONSTRAINT master_data_categories_eids_requirement_check CHECK (
        eids_requirement IN ('none', 'property', 'vehicle')
    );

COMMENT ON COLUMN master_data.categories.eids_requirement IS
    'Server-side listing EİDS requirement. not client-chosen. Development default none.';
