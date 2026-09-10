package businesses

import "math"

// earthRadiusKm is the WGS84 mean radius used by in-memory ST_DWithin equivalent.
// PostgreSQL matching uses geography ST_DWithin (spheroid meters).
const earthRadiusKm = 6371.0088

func haversineKm(a, b Coordinates) float64 {
	lat1 := a.Latitude * math.Pi / 180
	lat2 := b.Latitude * math.Pi / 180
	dLat := (b.Latitude - a.Latitude) * math.Pi / 180
	dLon := (b.Longitude - a.Longitude) * math.Pi / 180
	sinDLat := math.Sin(dLat / 2)
	sinDLon := math.Sin(dLon / 2)
	h := sinDLat*sinDLat + math.Cos(lat1)*math.Cos(lat2)*sinDLon*sinDLon
	return 2 * earthRadiusKm * math.Asin(math.Min(1, math.Sqrt(h)))
}

func roundDistanceKm(km float64) float64 {
	return math.Round(km*1000) / 1000
}
