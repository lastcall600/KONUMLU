CREATE SCHEMA master_data;

CREATE TABLE master_data.categories (
    id UUID PRIMARY KEY,
    code TEXT NOT NULL,
    parent_id UUID,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    CONSTRAINT master_data_categories_code_unique UNIQUE (code),
    CONSTRAINT master_data_categories_code_format CHECK (code ~ '^[a-z0-9][a-z0-9._-]*$'),
    CONSTRAINT master_data_categories_status_check CHECK (
        status IN ('draft', 'review', 'approved', 'published')
    ),
    CONSTRAINT master_data_categories_parent_fk FOREIGN KEY (parent_id)
        REFERENCES master_data.categories (id),
    CONSTRAINT master_data_categories_parent_not_self CHECK (parent_id IS NULL OR parent_id <> id),
    CONSTRAINT master_data_categories_updated_not_before_created CHECK (updated_at >= created_at),
    CONSTRAINT master_data_categories_published_at_check CHECK (
        (status = 'published' AND published_at IS NOT NULL)
        OR (status <> 'published' AND published_at IS NULL)
    )
);

CREATE TABLE master_data.category_labels (
    category_id UUID NOT NULL REFERENCES master_data.categories (id) ON DELETE CASCADE,
    locale TEXT NOT NULL,
    label TEXT NOT NULL,
    description TEXT,
    CONSTRAINT master_data_category_labels_pk PRIMARY KEY (category_id, locale),
    CONSTRAINT master_data_category_labels_locale_check CHECK (locale IN ('tr', 'en', 'ru', 'ar')),
    CONSTRAINT master_data_category_labels_label_not_empty CHECK (btrim(label) <> '')
);

CREATE TABLE master_data.category_schemas (
    id UUID PRIMARY KEY,
    category_id UUID NOT NULL REFERENCES master_data.categories (id),
    version INTEGER NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ,
    CONSTRAINT master_data_category_schemas_version_unique UNIQUE (category_id, version),
    CONSTRAINT master_data_category_schemas_version_positive CHECK (version > 0),
    CONSTRAINT master_data_category_schemas_status_check CHECK (
        status IN ('draft', 'review', 'approved', 'published')
    ),
    CONSTRAINT master_data_category_schemas_published_at_check CHECK (
        (status = 'published' AND published_at IS NOT NULL)
        OR (status <> 'published' AND published_at IS NULL)
    )
);

CREATE TABLE master_data.attributes (
    id UUID PRIMARY KEY,
    schema_id UUID NOT NULL REFERENCES master_data.category_schemas (id) ON DELETE CASCADE,
    code TEXT NOT NULL,
    value_type TEXT NOT NULL,
    required BOOLEAN NOT NULL,
    filterable BOOLEAN NOT NULL,
    sortable BOOLEAN NOT NULL,
    sort_order INTEGER NOT NULL,
    constraints JSONB NOT NULL,
    CONSTRAINT master_data_attributes_schema_code_unique UNIQUE (schema_id, code),
    CONSTRAINT master_data_attributes_code_format CHECK (code ~ '^[a-z0-9][a-z0-9._-]*$'),
    CONSTRAINT master_data_attributes_value_type_check CHECK (
        value_type IN ('text', 'integer', 'decimal', 'boolean', 'enum')
    ),
    CONSTRAINT master_data_attributes_sort_order_non_negative CHECK (sort_order >= 0),
    CONSTRAINT master_data_attributes_constraints_object CHECK (jsonb_typeof(constraints) = 'object')
);

CREATE TABLE master_data.attribute_labels (
    attribute_id UUID NOT NULL REFERENCES master_data.attributes (id) ON DELETE CASCADE,
    locale TEXT NOT NULL,
    label TEXT NOT NULL,
    help_text TEXT,
    CONSTRAINT master_data_attribute_labels_pk PRIMARY KEY (attribute_id, locale),
    CONSTRAINT master_data_attribute_labels_locale_check CHECK (locale IN ('tr', 'en', 'ru', 'ar')),
    CONSTRAINT master_data_attribute_labels_label_not_empty CHECK (btrim(label) <> '')
);

CREATE TABLE master_data.attribute_options (
    id UUID PRIMARY KEY,
    attribute_id UUID NOT NULL REFERENCES master_data.attributes (id) ON DELETE CASCADE,
    code TEXT NOT NULL,
    sort_order INTEGER NOT NULL,
    CONSTRAINT master_data_attribute_options_attr_code_unique UNIQUE (attribute_id, code),
    CONSTRAINT master_data_attribute_options_code_format CHECK (code ~ '^[a-z0-9][a-z0-9._-]*$'),
    CONSTRAINT master_data_attribute_options_sort_order_non_negative CHECK (sort_order >= 0)
);

CREATE TABLE master_data.attribute_option_labels (
    option_id UUID NOT NULL REFERENCES master_data.attribute_options (id) ON DELETE CASCADE,
    locale TEXT NOT NULL,
    label TEXT NOT NULL,
    CONSTRAINT master_data_attribute_option_labels_pk PRIMARY KEY (option_id, locale),
    CONSTRAINT master_data_attribute_option_labels_locale_check CHECK (locale IN ('tr', 'en', 'ru', 'ar')),
    CONSTRAINT master_data_attribute_option_labels_label_not_empty CHECK (btrim(label) <> '')
);

CREATE INDEX master_data_categories_parent_id_idx ON master_data.categories (parent_id);
CREATE INDEX master_data_categories_status_idx ON master_data.categories (status);
CREATE INDEX master_data_category_schemas_category_id_idx ON master_data.category_schemas (category_id);
CREATE INDEX master_data_attributes_schema_id_idx ON master_data.attributes (schema_id);
CREATE INDEX master_data_attribute_options_attribute_id_idx ON master_data.attribute_options (attribute_id);
