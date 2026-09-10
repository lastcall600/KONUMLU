ALTER TABLE businesses.services
    DROP COLUMN IF EXISTS category_id;

DROP INDEX IF EXISTS businesses_profiles_location_gix;

ALTER TABLE businesses.profiles
    DROP CONSTRAINT IF EXISTS businesses_profiles_longitude_range;

ALTER TABLE businesses.profiles
    DROP CONSTRAINT IF EXISTS businesses_profiles_latitude_range;

ALTER TABLE businesses.profiles
    DROP COLUMN IF EXISTS location;
