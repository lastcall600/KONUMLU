package listings

import (
	"context"
	"errors"

	locationcontracts "backend/internal/location/contracts"
	mediacontracts "backend/internal/media/contracts"
	"backend/internal/platform/db"
)

// Transactor starts a shared PostgreSQL transaction for listing+location+media writes.
type Transactor interface {
	Begin(ctx context.Context) (Tx, error)
}

// Tx is one unit of work. Context attaches the DB transaction when backed by PostgreSQL.
type Tx interface {
	Context(ctx context.Context) context.Context
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// DraftLocation is optional geo input. Coordinates are owned by Location after write.
type DraftLocation struct {
	Latitude          float64
	Longitude         float64
	CatalogLocationID *ID
}

// CreateListingDraftInput is seller-entered draft payload. Owner comes only from the session argument.
type CreateListingDraftInput struct {
	Content       DraftContent
	Location      *DraftLocation
	MediaAssetIDs []ID
	// OwnerUserID is ignored if a client ever populates a similarly named field elsewhere.
	// Session identity is the only owner authority.
}

// DraftOrchestrator composes Listings, Location, and Media around an authenticated owner.
type DraftOrchestrator struct {
	listings *Service
	geo      locationcontracts.Service
	media    mediacontracts.ListingMedia
	tx       Transactor
}

func NewDraftOrchestrator(listings *Service, geo locationcontracts.Service, media mediacontracts.ListingMedia, tx Transactor) (*DraftOrchestrator, error) {
	if listings == nil {
		return nil, errStoreRequired
	}
	return &DraftOrchestrator{listings: listings, geo: geo, media: media, tx: tx}, nil
}

func (o *DraftOrchestrator) CreateListingDraft(ctx context.Context, sessionUserID ID, in CreateListingDraftInput) (Listing, error) {
	if o == nil || o.listings == nil {
		return Listing{}, errStoreRequired
	}
	if sessionUserID.IsZero() {
		return Listing{}, errZeroID
	}
	if err := o.prepareMedia(ctx, sessionUserID, in.MediaAssetIDs); err != nil {
		return Listing{}, err
	}
	if in.Location != nil && o.geo == nil {
		return Listing{}, errUnavailable
	}

	var created Listing
	err := o.withTx(ctx, func(ctx context.Context) error {
		listing, err := o.listings.CreateDraft(ctx, sessionUserID, in.Content)
		if err != nil {
			return err
		}
		created = listing
		if in.Location != nil {
			if _, err := o.setLocation(ctx, listing.ID, *in.Location); err != nil {
				return err
			}
		}
		for _, assetID := range in.MediaAssetIDs {
			if _, err := o.attachMedia(ctx, sessionUserID, listing.ID, assetID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Listing{}, err
	}
	return created, nil
}

func (o *DraftOrchestrator) ReplaceOwnedLocation(ctx context.Context, sessionUserID, listingID ID, loc DraftLocation) (locationcontracts.ListingPoint, error) {
	if o == nil || o.listings == nil {
		return locationcontracts.ListingPoint{}, errStoreRequired
	}
	if o.geo == nil {
		return locationcontracts.ListingPoint{}, errUnavailable
	}
	if err := o.listings.assertOwnedBy(ctx, listingID, sessionUserID); err != nil {
		return locationcontracts.ListingPoint{}, err
	}
	var point locationcontracts.ListingPoint
	err := o.withTx(ctx, func(ctx context.Context) error {
		got, err := o.setLocation(ctx, listingID, loc)
		if err != nil {
			return err
		}
		point = got
		return nil
	})
	if err != nil {
		return locationcontracts.ListingPoint{}, err
	}
	return point, nil
}

func (o *DraftOrchestrator) AttachOwnedMedia(ctx context.Context, sessionUserID, listingID ID, assetIDs []ID) ([]mediacontracts.AssetRef, error) {
	if o == nil || o.listings == nil {
		return nil, errStoreRequired
	}
	if len(assetIDs) == 0 {
		return nil, errInvalidListing
	}
	if err := o.listings.assertOwnedBy(ctx, listingID, sessionUserID); err != nil {
		return nil, err
	}
	if err := o.prepareMedia(ctx, sessionUserID, assetIDs); err != nil {
		return nil, err
	}
	refs := make([]mediacontracts.AssetRef, 0, len(assetIDs))
	err := o.withTx(ctx, func(ctx context.Context) error {
		for _, assetID := range assetIDs {
			ref, err := o.attachMedia(ctx, sessionUserID, listingID, assetID)
			if err != nil {
				return err
			}
			refs = append(refs, ref)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return refs, nil
}

func (o *DraftOrchestrator) prepareMedia(ctx context.Context, ownerUserID ID, assetIDs []ID) error {
	if len(assetIDs) == 0 {
		return nil
	}
	if o.media == nil {
		return errUnavailable
	}
	seen := make(map[ID]struct{}, len(assetIDs))
	for _, id := range assetIDs {
		if id.IsZero() {
			return errZeroID
		}
		if _, dup := seen[id]; dup {
			return errInvalidListing
		}
		seen[id] = struct{}{}
		if err := o.media.AssertAttachable(ctx, mediacontracts.ID(id), mediacontracts.ID(ownerUserID)); err != nil {
			return mapMediaErr(err)
		}
	}
	return nil
}

func (o *DraftOrchestrator) setLocation(ctx context.Context, listingID ID, loc DraftLocation) (locationcontracts.ListingPoint, error) {
	scope, err := locationcontracts.NewListingWriteScope(locationcontracts.ID(listingID))
	if err != nil {
		return locationcontracts.ListingPoint{}, mapLocationErr(err)
	}
	var catalog *locationcontracts.ID
	if loc.CatalogLocationID != nil {
		id := locationcontracts.ID(*loc.CatalogLocationID)
		catalog = &id
	}
	point, err := o.geo.SetListingLocation(ctx, scope, locationcontracts.Coordinates{
		Latitude:  loc.Latitude,
		Longitude: loc.Longitude,
	}, catalog)
	if err != nil {
		return locationcontracts.ListingPoint{}, mapLocationErr(err)
	}
	return point, nil
}

func (o *DraftOrchestrator) attachMedia(ctx context.Context, ownerUserID, listingID, assetID ID) (mediacontracts.AssetRef, error) {
	ref, err := o.media.AttachToListing(ctx, mediacontracts.ID(assetID), mediacontracts.ListingAttachContext{
		ListingID:   mediacontracts.ID(listingID),
		ActorUserID: mediacontracts.ID(ownerUserID),
	})
	if err != nil {
		return mediacontracts.AssetRef{}, mapMediaErr(err)
	}
	return ref, nil
}

func (o *DraftOrchestrator) withTx(ctx context.Context, fn func(context.Context) error) error {
	if o.tx == nil {
		return fn(ctx)
	}
	tx, err := o.tx.Begin(ctx)
	if err != nil {
		return mapStoreErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx.Context(ctx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func mapLocationErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, locationcontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, locationcontracts.ErrInvalidLatitude) ||
		errors.Is(err, locationcontracts.ErrInvalidLongitude) ||
		errors.Is(err, locationcontracts.ErrInvalidCoordinate) {
		return err
	}
	return mapStoreErr(err)
}

func mapMediaErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, mediacontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, mediacontracts.ErrForbidden) {
		return errForbidden
	}
	if errors.Is(err, mediacontracts.ErrNotFound) {
		return errNotFound
	}
	if errors.Is(err, mediacontracts.ErrNotUsable) || errors.Is(err, mediacontracts.ErrAlreadyAttached) {
		return err
	}
	return mapStoreErr(err)
}

// PoolTransactor begins a PostgreSQL transaction shared by domain stores via db.WithTx.
type PoolTransactor struct {
	Pool *db.Pool
}

func (p PoolTransactor) Begin(ctx context.Context) (Tx, error) {
	if p.Pool == nil {
		return nil, errUnavailable
	}
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return poolTx{tx: tx}, nil
}

type poolTx struct {
	tx *db.Tx
}

func (t poolTx) Context(ctx context.Context) context.Context {
	return db.WithTx(ctx, t.tx)
}

func (t poolTx) Commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}

func (t poolTx) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}
