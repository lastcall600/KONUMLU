package search

import (
	"context"
	"errors"

	listingcontracts "backend/internal/listings/contracts"
	locationcontracts "backend/internal/location/contracts"
	"backend/internal/platform/db"
)

// Projector maintains the derived listing search documents.
type Projector struct {
	store    documentStore
	listings listingcontracts.SearchSource
	geo      locationcontracts.ListingLocationReader
}

func NewProjector(store documentStore, listings listingcontracts.SearchSource, geo locationcontracts.ListingLocationReader) (*Projector, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if listings == nil {
		return nil, errSourceRequired
	}
	if geo == nil {
		return nil, errSourceRequired
	}
	return &Projector{store: store, listings: listings, geo: geo}, nil
}

func (p *Projector) Upsert(ctx context.Context, doc ListingDocument) error {
	if p == nil || p.store == nil {
		return errStoreRequired
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	if !doc.Searchable() {
		return p.Remove(ctx, doc.ListingID)
	}
	if err := p.store.Upsert(ctx, doc); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func (p *Projector) Remove(ctx context.Context, listingID ID) error {
	if p == nil || p.store == nil {
		return errStoreRequired
	}
	if listingID.IsZero() {
		return errZeroID
	}
	if err := p.store.Remove(ctx, listingID); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func (p *Projector) Get(ctx context.Context, listingID ID) (ListingDocument, error) {
	if p == nil || p.store == nil {
		return ListingDocument{}, errStoreRequired
	}
	if listingID.IsZero() {
		return ListingDocument{}, errZeroID
	}
	doc, err := p.store.Get(ctx, listingID)
	if err != nil {
		return ListingDocument{}, mapStoreErr(err)
	}
	return doc, nil
}

func (p *Projector) Rebuild(ctx context.Context, listingID ID) error {
	if p == nil || p.store == nil || p.listings == nil || p.geo == nil {
		return errStoreRequired
	}
	if listingID.IsZero() {
		return errZeroID
	}
	snap, err := p.listings.GetListingSnapshot(ctx, listingcontracts.ID(listingID))
	if err != nil {
		if errors.Is(err, listingcontracts.ErrNotFound) {
			return p.Remove(ctx, listingID)
		}
		return mapSourceErr(err)
	}
	if !snap.PubliclyVisible() {
		return p.Remove(ctx, listingID)
	}
	doc, err := documentFromSnapshot(snap)
	if err != nil {
		return err
	}
	point, err := p.geo.GetListingLocation(ctx, locationcontracts.ID(listingID))
	if err != nil && !errors.Is(err, locationcontracts.ErrNotFound) {
		return mapSourceErr(err)
	}
	if err == nil {
		lat := point.Latitude
		lon := point.Longitude
		doc.Latitude = &lat
		doc.Longitude = &lon
		if point.CatalogLocationID != nil {
			id := ID(*point.CatalogLocationID)
			doc.CatalogLocationID = &id
		}
	}
	return p.Upsert(ctx, doc)
}

func documentFromSnapshot(snap listingcontracts.ListingSnapshot) (ListingDocument, error) {
	doc := ListingDocument{
		ListingID:             ID(snap.ID),
		Status:                snap.Status,
		ModerationState:       snap.ModerationState,
		CategoryID:            ID(snap.CategoryID),
		CategorySchemaVersion: snap.CategorySchemaVersion,
		Title:                 snap.Title,
		Description:           snap.Description,
		PriceAmount:           snap.PriceAmount,
		PriceCurrency:         snap.PriceCurrency,
		Attributes:            snap.Attributes,
		PublishedAt:           snap.PublishedAt,
		UpdatedAt:             snap.UpdatedAt,
	}
	if doc.Attributes == nil {
		doc.Attributes = map[string]any{}
	}
	if err := doc.Validate(); err != nil {
		return ListingDocument{}, err
	}
	return doc, nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, db.ErrUnavailable) {
		return errUnavailable
	}
	if errors.Is(err, errStoreRequired) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidDoc) || errors.Is(err, errInvalidEvent) ||
		errors.Is(err, errInvalidQuery) || errors.Is(err, errSourceRequired) {
		return err
	}
	return errUnavailable
}

func mapSourceErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, listingcontracts.ErrZeroID) || errors.Is(err, locationcontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, listingcontracts.ErrNotFound) || errors.Is(err, locationcontracts.ErrNotFound) {
		return errNotFound
	}
	return errUnavailable
}
