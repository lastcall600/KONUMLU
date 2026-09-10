ALTER TABLE businesses.profiles
    ADD COLUMN location geography(Point, 4326);

ALTER TABLE businesses.profiles
    ADD CONSTRAINT businesses_profiles_latitude_range CHECK (
        location IS NULL OR (ST_Y(location::geometry) >= -90 AND ST_Y(location::geometry) <= 90)
    );

ALTER TABLE businesses.profiles
    ADD CONSTRAINT businesses_profiles_longitude_range CHECK (
        location IS NULL OR (ST_X(location::geometry) >= -180 AND ST_X(location::geometry) <= 180)
    );

CREATE INDEX businesses_profiles_location_gix
    ON businesses.profiles
    USING GIST (location)
    WHERE location IS NOT NULL;

ALTER TABLE businesses.services
    ADD COLUMN category_id UUID;
