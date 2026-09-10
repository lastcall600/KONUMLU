CREATE SCHEMA location;

CREATE TABLE location.listing_locations (
    listing_id UUID PRIMARY KEY,
    catalog_location_id UUID,
    point geography(Point, 4326) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT location_listing_locations_updated_not_before_created CHECK (updated_at >= created_at),
    CONSTRAINT location_listing_locations_latitude_range CHECK (
        ST_Y(point::geometry) >= -90 AND ST_Y(point::geometry) <= 90
    ),
    CONSTRAINT location_listing_locations_longitude_range CHECK (
        ST_X(point::geometry) >= -180 AND ST_X(point::geometry) <= 180
    )
);
