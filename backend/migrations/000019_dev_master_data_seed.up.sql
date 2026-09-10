-- DEVELOPMENT SEED ONLY
-- Not official Türkiye taxonomy. Not a legal, EİDS, or geography source.
-- No provinces, districts, Fethiye, or official codes/sources.
-- Fixed UUIDs exist only so local listing-form tests can resolve published categories.

-- Categories (published)
--   Emlak  0b500001-0000-4000-a000-000000000001  code=dev.emlak
--   Araç   0b500001-0000-4000-a000-000000000002  code=dev.arac
--   Hizmet 0b500001-0000-4000-a000-000000000003  code=dev.hizmet
-- Schema v1 (published)
--   Emlak  0b500001-0000-4000-a000-000000000011
--   Araç   0b500001-0000-4000-a000-000000000012
--   Hizmet 0b500001-0000-4000-a000-000000000013

INSERT INTO master_data.categories (
    id, code, parent_id, status, created_at, updated_at, published_at
) VALUES
    (
        '0b500001-0000-4000-a000-000000000001'::uuid,
        'dev.emlak',
        NULL,
        'published',
        TIMESTAMPTZ '2026-09-06 18:00:00+00',
        TIMESTAMPTZ '2026-09-06 18:00:00+00',
        TIMESTAMPTZ '2026-09-06 18:00:00+00'
    ),
    (
        '0b500001-0000-4000-a000-000000000002'::uuid,
        'dev.arac',
        NULL,
        'published',
        TIMESTAMPTZ '2026-09-06 18:00:00+00',
        TIMESTAMPTZ '2026-09-06 18:00:00+00',
        TIMESTAMPTZ '2026-09-06 18:00:00+00'
    ),
    (
        '0b500001-0000-4000-a000-000000000003'::uuid,
        'dev.hizmet',
        NULL,
        'published',
        TIMESTAMPTZ '2026-09-06 18:00:00+00',
        TIMESTAMPTZ '2026-09-06 18:00:00+00',
        TIMESTAMPTZ '2026-09-06 18:00:00+00'
    );

INSERT INTO master_data.category_labels (category_id, locale, label, description) VALUES
    ('0b500001-0000-4000-a000-000000000001'::uuid, 'tr', 'Emlak', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000001'::uuid, 'en', 'Real estate', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000001'::uuid, 'ru', 'Недвижимость', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000001'::uuid, 'ar', 'عقارات', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000002'::uuid, 'tr', 'Araç', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000002'::uuid, 'en', 'Vehicle', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000002'::uuid, 'ru', 'Транспорт', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000002'::uuid, 'ar', 'مركبة', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000003'::uuid, 'tr', 'Hizmet', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000003'::uuid, 'en', 'Service', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000003'::uuid, 'ru', 'Услуга', 'DEVELOPMENT SEED ONLY'),
    ('0b500001-0000-4000-a000-000000000003'::uuid, 'ar', 'خدمة', 'DEVELOPMENT SEED ONLY');

INSERT INTO master_data.category_schemas (
    id, category_id, version, status, created_at, published_at
) VALUES
    (
        '0b500001-0000-4000-a000-000000000011'::uuid,
        '0b500001-0000-4000-a000-000000000001'::uuid,
        1,
        'published',
        TIMESTAMPTZ '2026-09-06 18:00:00+00',
        TIMESTAMPTZ '2026-09-06 18:00:00+00'
    ),
    (
        '0b500001-0000-4000-a000-000000000012'::uuid,
        '0b500001-0000-4000-a000-000000000002'::uuid,
        1,
        'published',
        TIMESTAMPTZ '2026-09-06 18:00:00+00',
        TIMESTAMPTZ '2026-09-06 18:00:00+00'
    ),
    (
        '0b500001-0000-4000-a000-000000000013'::uuid,
        '0b500001-0000-4000-a000-000000000003'::uuid,
        1,
        'published',
        TIMESTAMPTZ '2026-09-06 18:00:00+00',
        TIMESTAMPTZ '2026-09-06 18:00:00+00'
    );

-- DEVELOPMENT SEED ONLY — Emlak form v1
INSERT INTO master_data.attributes (
    id, schema_id, code, value_type, required, filterable, sortable, sort_order, constraints
) VALUES
    (
        '0b500001-0000-4000-a000-000000000101'::uuid,
        '0b500001-0000-4000-a000-000000000011'::uuid,
        'property_type',
        'enum',
        TRUE,
        FALSE,
        FALSE,
        0,
        '{}'::jsonb
    ),
    (
        '0b500001-0000-4000-a000-000000000102'::uuid,
        '0b500001-0000-4000-a000-000000000011'::uuid,
        'rooms',
        'integer',
        FALSE,
        FALSE,
        TRUE,
        1,
        '{"min": 0}'::jsonb
    ),
    (
        '0b500001-0000-4000-a000-000000000103'::uuid,
        '0b500001-0000-4000-a000-000000000011'::uuid,
        'area_m2',
        'decimal',
        FALSE,
        FALSE,
        TRUE,
        2,
        '{"min": 0}'::jsonb
    );

-- DEVELOPMENT SEED ONLY — Araç form v1
INSERT INTO master_data.attributes (
    id, schema_id, code, value_type, required, filterable, sortable, sort_order, constraints
) VALUES
    (
        '0b500001-0000-4000-a000-000000000201'::uuid,
        '0b500001-0000-4000-a000-000000000012'::uuid,
        'vehicle_type',
        'enum',
        TRUE,
        FALSE,
        FALSE,
        0,
        '{}'::jsonb
    ),
    (
        '0b500001-0000-4000-a000-000000000202'::uuid,
        '0b500001-0000-4000-a000-000000000012'::uuid,
        'model_year',
        'integer',
        FALSE,
        FALSE,
        TRUE,
        1,
        '{"min": 1900}'::jsonb
    ),
    (
        '0b500001-0000-4000-a000-000000000203'::uuid,
        '0b500001-0000-4000-a000-000000000012'::uuid,
        'mileage_km',
        'integer',
        FALSE,
        FALSE,
        TRUE,
        2,
        '{"min": 0}'::jsonb
    );

-- DEVELOPMENT SEED ONLY — Hizmet form v1
INSERT INTO master_data.attributes (
    id, schema_id, code, value_type, required, filterable, sortable, sort_order, constraints
) VALUES
    (
        '0b500001-0000-4000-a000-000000000301'::uuid,
        '0b500001-0000-4000-a000-000000000013'::uuid,
        'service_type',
        'text',
        TRUE,
        FALSE,
        TRUE,
        0,
        '{"max_length": 80}'::jsonb
    ),
    (
        '0b500001-0000-4000-a000-000000000302'::uuid,
        '0b500001-0000-4000-a000-000000000013'::uuid,
        'experience_years',
        'integer',
        FALSE,
        FALSE,
        TRUE,
        1,
        '{"min": 0}'::jsonb
    );

INSERT INTO master_data.attribute_labels (attribute_id, locale, label, help_text) VALUES
    ('0b500001-0000-4000-a000-000000000101'::uuid, 'tr', 'Mülk tipi', NULL),
    ('0b500001-0000-4000-a000-000000000101'::uuid, 'en', 'Property type', NULL),
    ('0b500001-0000-4000-a000-000000000101'::uuid, 'ru', 'Тип недвижимости', NULL),
    ('0b500001-0000-4000-a000-000000000101'::uuid, 'ar', 'نوع العقار', NULL),
    ('0b500001-0000-4000-a000-000000000102'::uuid, 'tr', 'Oda sayısı', NULL),
    ('0b500001-0000-4000-a000-000000000102'::uuid, 'en', 'Rooms', NULL),
    ('0b500001-0000-4000-a000-000000000102'::uuid, 'ru', 'Комнаты', NULL),
    ('0b500001-0000-4000-a000-000000000102'::uuid, 'ar', 'غرف', NULL),
    ('0b500001-0000-4000-a000-000000000103'::uuid, 'tr', 'Alan (m²)', NULL),
    ('0b500001-0000-4000-a000-000000000103'::uuid, 'en', 'Area (m²)', NULL),
    ('0b500001-0000-4000-a000-000000000103'::uuid, 'ru', 'Площадь (м²)', NULL),
    ('0b500001-0000-4000-a000-000000000103'::uuid, 'ar', 'المساحة (م²)', NULL),
    ('0b500001-0000-4000-a000-000000000201'::uuid, 'tr', 'Araç tipi', NULL),
    ('0b500001-0000-4000-a000-000000000201'::uuid, 'en', 'Vehicle type', NULL),
    ('0b500001-0000-4000-a000-000000000201'::uuid, 'ru', 'Тип транспорта', NULL),
    ('0b500001-0000-4000-a000-000000000201'::uuid, 'ar', 'نوع المركبة', NULL),
    ('0b500001-0000-4000-a000-000000000202'::uuid, 'tr', 'Model yılı', NULL),
    ('0b500001-0000-4000-a000-000000000202'::uuid, 'en', 'Model year', NULL),
    ('0b500001-0000-4000-a000-000000000202'::uuid, 'ru', 'Год выпуска', NULL),
    ('0b500001-0000-4000-a000-000000000202'::uuid, 'ar', 'سنة الصنع', NULL),
    ('0b500001-0000-4000-a000-000000000203'::uuid, 'tr', 'Kilometre', NULL),
    ('0b500001-0000-4000-a000-000000000203'::uuid, 'en', 'Mileage (km)', NULL),
    ('0b500001-0000-4000-a000-000000000203'::uuid, 'ru', 'Пробег (км)', NULL),
    ('0b500001-0000-4000-a000-000000000203'::uuid, 'ar', 'المسافة (كم)', NULL),
    ('0b500001-0000-4000-a000-000000000301'::uuid, 'tr', 'Hizmet tipi', NULL),
    ('0b500001-0000-4000-a000-000000000301'::uuid, 'en', 'Service type', NULL),
    ('0b500001-0000-4000-a000-000000000301'::uuid, 'ru', 'Тип услуги', NULL),
    ('0b500001-0000-4000-a000-000000000301'::uuid, 'ar', 'نوع الخدمة', NULL),
    ('0b500001-0000-4000-a000-000000000302'::uuid, 'tr', 'Deneyim (yıl)', NULL),
    ('0b500001-0000-4000-a000-000000000302'::uuid, 'en', 'Experience (years)', NULL),
    ('0b500001-0000-4000-a000-000000000302'::uuid, 'ru', 'Опыт (лет)', NULL),
    ('0b500001-0000-4000-a000-000000000302'::uuid, 'ar', 'الخبرة (سنوات)', NULL);

INSERT INTO master_data.attribute_options (id, attribute_id, code, sort_order) VALUES
    ('0b500001-0000-4000-a000-000000000111'::uuid, '0b500001-0000-4000-a000-000000000101'::uuid, 'apartment', 0),
    ('0b500001-0000-4000-a000-000000000112'::uuid, '0b500001-0000-4000-a000-000000000101'::uuid, 'villa', 1),
    ('0b500001-0000-4000-a000-000000000211'::uuid, '0b500001-0000-4000-a000-000000000201'::uuid, 'car', 0),
    ('0b500001-0000-4000-a000-000000000212'::uuid, '0b500001-0000-4000-a000-000000000201'::uuid, 'motorcycle', 1);

INSERT INTO master_data.attribute_option_labels (option_id, locale, label) VALUES
    ('0b500001-0000-4000-a000-000000000111'::uuid, 'tr', 'Daire'),
    ('0b500001-0000-4000-a000-000000000111'::uuid, 'en', 'Apartment'),
    ('0b500001-0000-4000-a000-000000000111'::uuid, 'ru', 'Квартира'),
    ('0b500001-0000-4000-a000-000000000111'::uuid, 'ar', 'شقة'),
    ('0b500001-0000-4000-a000-000000000112'::uuid, 'tr', 'Villa'),
    ('0b500001-0000-4000-a000-000000000112'::uuid, 'en', 'Villa'),
    ('0b500001-0000-4000-a000-000000000112'::uuid, 'ru', 'Вилла'),
    ('0b500001-0000-4000-a000-000000000112'::uuid, 'ar', 'فيلا'),
    ('0b500001-0000-4000-a000-000000000211'::uuid, 'tr', 'Otomobil'),
    ('0b500001-0000-4000-a000-000000000211'::uuid, 'en', 'Car'),
    ('0b500001-0000-4000-a000-000000000211'::uuid, 'ru', 'Автомобиль'),
    ('0b500001-0000-4000-a000-000000000211'::uuid, 'ar', 'سيارة'),
    ('0b500001-0000-4000-a000-000000000212'::uuid, 'tr', 'Motosiklet'),
    ('0b500001-0000-4000-a000-000000000212'::uuid, 'en', 'Motorcycle'),
    ('0b500001-0000-4000-a000-000000000212'::uuid, 'ru', 'Мотоцикл'),
    ('0b500001-0000-4000-a000-000000000212'::uuid, 'ar', 'دراجة نارية');
