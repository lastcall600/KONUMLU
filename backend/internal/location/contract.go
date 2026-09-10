package location

import (
	"context"
	"errors"

	"backend/internal/location/contracts"
)

var _ contracts.Service = (*geoAPI)(nil)
var _ contracts.ListingLocationReader = (*geoAPI)(nil)

type geoAPI struct {
	svc *Service
}

// NewGeo exposes Location through its contract. Other domains must use this, not package location.
func NewGeo(svc *Service) contracts.Service {
	return geoAPI{svc: svc}
}

func (g geoAPI) SetListingLocation(ctx context.Context, scope contracts.ListingWriteScope, coords contracts.Coordinates, catalogLocationID *contracts.ID) (contracts.ListingPoint, error) {
	innerScope, err := NewListingWriteScope(ID(scope.ListingID))
	if err != nil {
		return contracts.ListingPoint{}, mapContractErr(err)
	}
	var catalog *ID
	if catalogLocationID != nil {
		id := ID(*catalogLocationID)
		catalog = &id
	}
	loc, err := g.svc.SetListingLocation(ctx, innerScope, Coordinates{
		Latitude:  coords.Latitude,
		Longitude: coords.Longitude,
	}, catalog)
	if err != nil {
		return contracts.ListingPoint{}, mapContractErr(err)
	}
	return toContractPoint(loc), nil
}

func (g geoAPI) GetListingLocation(ctx context.Context, listingID contracts.ID) (contracts.ListingPoint, error) {
	if g.svc == nil {
		return contracts.ListingPoint{}, contracts.ErrUnavailable
	}
	loc, err := g.svc.GetByListingID(ctx, ID(listingID))
	if err != nil {
		return contracts.ListingPoint{}, mapContractErr(err)
	}
	return toContractPoint(loc), nil
}

// NewListingLocations exposes listing geo reads through the contract.
func NewListingLocations(svc *Service) contracts.ListingLocationReader {
	return geoAPI{svc: svc}
}

func toContractPoint(loc ListingLocation) contracts.ListingPoint {
	p := contracts.ListingPoint{
		ListingID: contracts.ID(loc.ListingID),
		Latitude:  loc.Latitude,
		Longitude: loc.Longitude,
	}
	if loc.CatalogLocationID != nil {
		id := contracts.ID(*loc.CatalogLocationID)
		p.CatalogLocationID = &id
	}
	return p
}

func mapContractErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) {
		return contracts.ErrZeroID
	}
	if errors.Is(err, errNotFound) {
		return contracts.ErrNotFound
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, errStoreRequired) {
		return contracts.ErrUnavailable
	}
	if errors.Is(err, errInvalidLatitude) {
		return contracts.ErrInvalidLatitude
	}
	if errors.Is(err, errInvalidLongitude) {
		return contracts.ErrInvalidLongitude
	}
	if errors.Is(err, errInvalidCoordinate) {
		return contracts.ErrInvalidCoordinate
	}
	return err
}
