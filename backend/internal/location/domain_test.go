package location

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestValidCoordinate(t *testing.T) {
	coords := Coordinates{Latitude: 36.621, Longitude: 29.116}
	if err := ValidateCoordinates(coords); err != nil {
		t.Fatal(err)
	}
	listingID := mustID(t)
	now := time.Date(2026, 9, 6, 15, 0, 0, 0, time.UTC)
	loc, err := NewListingLocation(listingID, coords, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Latitude != 36.621 || loc.Longitude != 29.116 {
		t.Fatalf("coords = %v %v", loc.Latitude, loc.Longitude)
	}
}

func TestInvalidLatitude(t *testing.T) {
	cases := []Coordinates{
		{Latitude: 90.0001, Longitude: 0},
		{Latitude: -90.0001, Longitude: 0},
		{Latitude: math.NaN(), Longitude: 0},
		{Latitude: math.Inf(1), Longitude: 0},
	}
	for _, c := range cases {
		if err := ValidateCoordinates(c); !errors.Is(err, errInvalidLatitude) {
			t.Fatalf("coords %+v err = %v", c, err)
		}
	}
}

func TestInvalidLongitude(t *testing.T) {
	cases := []Coordinates{
		{Latitude: 0, Longitude: 180.0001},
		{Latitude: 0, Longitude: -180.0001},
		{Latitude: 0, Longitude: math.NaN()},
		{Latitude: 0, Longitude: math.Inf(-1)},
	}
	for _, c := range cases {
		if err := ValidateCoordinates(c); !errors.Is(err, errInvalidLongitude) {
			t.Fatalf("coords %+v err = %v", c, err)
		}
	}
}

func TestLatitudeLongitudeBoundsInclusive(t *testing.T) {
	for _, c := range []Coordinates{
		{Latitude: 90, Longitude: 180},
		{Latitude: -90, Longitude: -180},
	} {
		if err := ValidateCoordinates(c); err != nil {
			t.Fatalf("bounds %+v err = %v", c, err)
		}
	}
}

func TestPointWriteArgsLongitudeFirst(t *testing.T) {
	lon, lat := pointWriteArgs(Coordinates{Latitude: 36.621, Longitude: 29.116})
	if lon != 29.116 || lat != 36.621 {
		t.Fatalf("ST_MakePoint args must be longitude, latitude; got lon=%v lat=%v", lon, lat)
	}
	if !strings.Contains(writePointSQL, "ST_MakePoint($3, $4)") {
		t.Fatalf("write SQL = %s", writePointSQL)
	}
	if !strings.Contains(writePointSQL, "4326") {
		t.Fatalf("SRID missing: %s", writePointSQL)
	}
	if !strings.Contains(selectListingLocationSQL, "ST_Y(point::geometry) AS latitude") {
		t.Fatalf("read lat SQL = %s", selectListingLocationSQL)
	}
	if !strings.Contains(selectListingLocationSQL, "ST_X(point::geometry) AS longitude") {
		t.Fatalf("read lon SQL = %s", selectListingLocationSQL)
	}
	if strings.Contains(strings.ToLower(writePointSQL+selectListingLocationSQL), "h3") ||
		strings.Contains(strings.ToLower(writePointSQL+selectListingLocationSQL), "geohash") {
		t.Fatal("h3/geohash must not be source of truth")
	}
}

func TestZeroListingIDRejected(t *testing.T) {
	_, err := NewListingLocation(ID{}, Coordinates{Latitude: 0, Longitude: 0}, nil, time.Now().UTC())
	if !errors.Is(err, errZeroID) {
		t.Fatalf("err = %v", err)
	}
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
