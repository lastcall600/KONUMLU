package contracts

import (
	"context"
	"errors"
)

var (
	ErrZeroID          = errors.New("media id must not be zero")
	ErrForbidden       = errors.New("media access denied")
	ErrNotUsable       = errors.New("media asset is not usable")
	ErrAlreadyAttached = errors.New("media asset already attached")
	ErrNotFound        = errors.New("media asset not found")
	ErrUnavailable     = errors.New("media unavailable")
)

// ID is a media asset, listing, or user UUID as understood by Media.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// ListingAttachContext is caller-provided, already-validated listing ownership.
// Media checks asset ownership and attach rules only. It does not query Listings.
type ListingAttachContext struct {
	ListingID   ID
	ActorUserID ID
}

// AssetRef is the minimal attach result.
type AssetRef struct {
	ID          ID
	OwnerUserID ID
	ListingID   *ID
	Status      string
}

// ListingMedia is the Media surface used by listing orchestration.
type ListingMedia interface {
	AssertAttachable(ctx context.Context, assetID, ownerUserID ID) error
	AttachToListing(ctx context.Context, assetID ID, bind ListingAttachContext) (AssetRef, error)
}

// PublicMediaItem is public-safe listing media metadata. It must not include
// object keys, bucket details, owner internals, or processing/moderation fields.
type PublicMediaItem struct {
	AssetID ID
	Kind    string
	Order   int
	URL     string
	Width   *int
	Height  *int
}

// PublicListingMedia is the Media read surface used by public listing detail.
type PublicListingMedia interface {
	ListPublicListingMedia(ctx context.Context, listingID ID) ([]PublicMediaItem, error)
}
