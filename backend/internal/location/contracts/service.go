package contracts

import (
	"context"
	"errors"
)

var (
	ErrZeroID            = errors.New("location id must not be zero")
	ErrInvalidLatitude   = errors.New("invalid latitude")
	ErrInvalidLongitude  = errors.New("invalid longitude")
	ErrInvalidCoordinate = errors.New("invalid coordinate")
	ErrNotFound          = errors.New("listing location not found")
	ErrUnavailable       = errors.New("location unavailable")
	ErrInvalidEvent      = errors.New("invalid location event")
)

// ID is a listing or catalog location UUID as understood by Location.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// Coordinates are WGS84 decimal degrees. Location owns geo values.
type Coordinates struct {
	Latitude  float64
	Longitude float64
}

// ListingWriteScope is caller-provided proof that listing ownership was already
// validated. Location does not resolve listing owners or import Listings.
type ListingWriteScope struct {
	ListingID ID
}

func NewListingWriteScope(listingID ID) (ListingWriteScope, error) {
	if listingID.IsZero() {
		return ListingWriteScope{}, ErrZeroID
	}
	return ListingWriteScope{ListingID: listingID}, nil
}

// ListingPoint is the active geo point for a listing.
type ListingPoint struct {
	ListingID         ID
	CatalogLocationID *ID
	Latitude          float64
	Longitude         float64
}

// Service is the Location write surface used by listing orchestration.
type Service interface {
	SetListingLocation(ctx context.Context, scope ListingWriteScope, coords Coordinates, catalogLocationID *ID) (ListingPoint, error)
}

// ListingLocationReader is the Location read Search may call to rebuild geo.
type ListingLocationReader interface {
	GetListingLocation(ctx context.Context, listingID ID) (ListingPoint, error)
}
