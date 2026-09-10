package location

import (
	"context"
	"errors"

	"backend/internal/platform/db"
)

var _ listingLocationStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

func (p *PostgresStore) Upsert(ctx context.Context, loc ListingLocation) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := loc.Validate(); err != nil {
		return err
	}
	lon, lat := pointWriteArgs(loc.Coordinates())
	_, err := p.db.Exec(ctx, upsertListingLocationSQL,
		loc.ListingID, catalogArg(loc.CatalogLocationID), lon, lat, loc.CreatedAt, loc.UpdatedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) GetByListingID(ctx context.Context, listingID ID) (ListingLocation, error) {
	if p == nil || p.db == nil {
		return ListingLocation{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, selectListingLocationSQL, listingID)
	got, err := scanListingLocation(row)
	if err != nil {
		return ListingLocation{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) DeleteByListingID(ctx context.Context, listingID ID) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	_, err := p.db.Exec(ctx, deleteListingLocationSQL, listingID)
	return mapDBErr(err)
}

func catalogArg(id *ID) any {
	if id == nil {
		return nil
	}
	return *id
}

func scanListingLocation(row interface {
	Scan(dest ...any) error
}) (ListingLocation, error) {
	var loc ListingLocation
	var catalog *ID
	if err := row.Scan(
		&loc.ListingID, &catalog, &loc.Latitude, &loc.Longitude, &loc.CreatedAt, &loc.UpdatedAt,
	); err != nil {
		return ListingLocation{}, err
	}
	loc.CatalogLocationID = cloneCatalogID(catalog)
	return loc, nil
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
