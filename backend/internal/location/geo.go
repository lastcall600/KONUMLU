package location

// PostGIS ST_MakePoint is (longitude, latitude) — X then Y — in SRID 4326.
// Callers must not reverse this order. H3 and geohash are not sources of truth.

const (
	writePointSQL = `ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography`
	readLatSQL    = `ST_Y(point::geometry)`
	readLonSQL    = `ST_X(point::geometry)`
)

const upsertListingLocationSQL = `
INSERT INTO location.listing_locations (
	listing_id, catalog_location_id, point, created_at, updated_at
) VALUES (
	$1, $2, ` + writePointSQL + `, $5, $6
)
ON CONFLICT (listing_id) DO UPDATE SET
	catalog_location_id = EXCLUDED.catalog_location_id,
	point = EXCLUDED.point,
	updated_at = EXCLUDED.updated_at`

const selectListingLocationSQL = `
SELECT listing_id, catalog_location_id,
	` + readLatSQL + ` AS latitude,
	` + readLonSQL + ` AS longitude,
	created_at, updated_at
FROM location.listing_locations
WHERE listing_id = $1`

const deleteListingLocationSQL = `
DELETE FROM location.listing_locations
WHERE listing_id = $1`

// pointWriteArgs returns ST_MakePoint arguments in PostGIS order: longitude, latitude.
func pointWriteArgs(c Coordinates) (longitude, latitude float64) {
	return c.Longitude, c.Latitude
}
