package location

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	errZeroID            = errors.New("location id must not be zero")
	errInvalidCoordinate = errors.New("invalid coordinate")
	errInvalidLatitude   = errors.New("invalid latitude")
	errInvalidLongitude  = errors.New("invalid longitude")
	errInvalidLocation   = errors.New("invalid listing location")
	errStoreRequired     = errors.New("location store required")
	errUnavailable       = errors.New("location unavailable")
	errNotFound          = errors.New("listing location not found")
)

// Exported sentinels for tests and later HTTP adapters.
var (
	ErrZeroID            = errZeroID
	ErrInvalidCoordinate = errInvalidCoordinate
	ErrInvalidLatitude   = errInvalidLatitude
	ErrInvalidLongitude  = errInvalidLongitude
	ErrInvalidLocation   = errInvalidLocation
	ErrStoreRequired     = errStoreRequired
	ErrUnavailable       = errUnavailable
	ErrNotFound          = errNotFound
)

// WGS84SRID is EPSG:4326. Canonical listing geo is PostGIS geography in this SRID.
const WGS84SRID = 4326

// ID is an application-generated UUID. Location does not mint listing IDs.
type ID [16]byte

func NewID() (ID, error) {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		return ID{}, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id, nil
}

func ParseID(s string) (ID, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(s) != 32 {
		return ID{}, errInvalidLocation
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidLocation
	}
	var id ID
	copy(id[:], b)
	if id.IsZero() {
		return ID{}, errZeroID
	}
	return id, nil
}

func (id ID) IsZero() bool {
	return id == ID{}
}

func (id ID) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:])
}

// Coordinates are WGS84 decimal degrees. Location owns these values; Listings does not.
type Coordinates struct {
	Latitude  float64
	Longitude float64
}

func ValidateCoordinates(c Coordinates) error {
	if math.IsNaN(c.Latitude) || math.IsInf(c.Latitude, 0) {
		return errInvalidLatitude
	}
	if math.IsNaN(c.Longitude) || math.IsInf(c.Longitude, 0) {
		return errInvalidLongitude
	}
	if c.Latitude < -90 || c.Latitude > 90 {
		return errInvalidLatitude
	}
	if c.Longitude < -180 || c.Longitude > 180 {
		return errInvalidLongitude
	}
	return nil
}

// ListingLocation is the single active geo point for a listing (V1).
// catalog_location_id is a Master Data catalog reference only — no FK yet.
type ListingLocation struct {
	ListingID         ID
	CatalogLocationID *ID
	Latitude          float64
	Longitude         float64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (l ListingLocation) Coordinates() Coordinates {
	return Coordinates{Latitude: l.Latitude, Longitude: l.Longitude}
}

func (l ListingLocation) Validate() error {
	if l.ListingID.IsZero() {
		return errZeroID
	}
	if l.CatalogLocationID != nil && l.CatalogLocationID.IsZero() {
		return errZeroID
	}
	if err := ValidateCoordinates(l.Coordinates()); err != nil {
		return err
	}
	if l.CreatedAt.IsZero() || l.UpdatedAt.Before(l.CreatedAt) {
		return errInvalidLocation
	}
	return nil
}

// ListingWriteScope is constructed after listing ownership has been validated
// by the caller. Location does not import Listings or resolve owners.
type ListingWriteScope struct {
	ListingID ID
}

func NewListingWriteScope(listingID ID) (ListingWriteScope, error) {
	if listingID.IsZero() {
		return ListingWriteScope{}, errZeroID
	}
	return ListingWriteScope{ListingID: listingID}, nil
}

func NewListingLocation(listingID ID, coords Coordinates, catalogLocationID *ID, now time.Time) (ListingLocation, error) {
	if listingID.IsZero() {
		return ListingLocation{}, errZeroID
	}
	if catalogLocationID != nil && catalogLocationID.IsZero() {
		return ListingLocation{}, errZeroID
	}
	if now.IsZero() {
		return ListingLocation{}, errInvalidLocation
	}
	if err := ValidateCoordinates(coords); err != nil {
		return ListingLocation{}, err
	}
	var catalog *ID
	if catalogLocationID != nil {
		id := *catalogLocationID
		catalog = &id
	}
	loc := ListingLocation{
		ListingID:         listingID,
		CatalogLocationID: catalog,
		Latitude:          coords.Latitude,
		Longitude:         coords.Longitude,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := loc.Validate(); err != nil {
		return ListingLocation{}, err
	}
	return loc, nil
}

func cloneCatalogID(id *ID) *ID {
	if id == nil {
		return nil
	}
	cp := *id
	return &cp
}

func cloneListingLocation(l ListingLocation) ListingLocation {
	l.CatalogLocationID = cloneCatalogID(l.CatalogLocationID)
	return l
}
