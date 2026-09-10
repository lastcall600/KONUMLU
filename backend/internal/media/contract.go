package media

import (
	"context"
	"errors"

	"backend/internal/media/contracts"
)

var _ contracts.ListingMedia = (*listingMediaAPI)(nil)
var _ contracts.PublicListingMedia = (*publicListingMediaAPI)(nil)

type listingMediaAPI struct {
	svc *Service
}

// NewListingMedia exposes Media attach operations through the contract.
func NewListingMedia(svc *Service) contracts.ListingMedia {
	return listingMediaAPI{svc: svc}
}

func (a listingMediaAPI) AssertAttachable(ctx context.Context, assetID, ownerUserID contracts.ID) error {
	return mapMediaContractErr(a.svc.AssertAttachable(ctx, ID(assetID), ID(ownerUserID)))
}

func (a listingMediaAPI) AttachToListing(ctx context.Context, assetID contracts.ID, bind contracts.ListingAttachContext) (contracts.AssetRef, error) {
	current, err := a.svc.Get(ctx, ID(assetID), ID(bind.ActorUserID))
	if err != nil {
		return contracts.AssetRef{}, mapMediaContractErr(err)
	}
	asset, err := a.svc.AttachListing(ctx, ID(assetID), ListingBind{
		ListingID:   ID(bind.ListingID),
		ActorUserID: ID(bind.ActorUserID),
	}, current.UpdatedAt)
	if err != nil {
		return contracts.AssetRef{}, mapMediaContractErr(err)
	}
	return toAssetRef(asset), nil
}

type publicListingMediaAPI struct {
	svc *Service
}

// NewPublicListingMedia exposes public-safe processed listing media through the contract.
func NewPublicListingMedia(svc *Service) contracts.PublicListingMedia {
	return publicListingMediaAPI{svc: svc}
}

func (a publicListingMediaAPI) ListPublicListingMedia(ctx context.Context, listingID contracts.ID) ([]contracts.PublicMediaItem, error) {
	if a.svc == nil {
		return []contracts.PublicMediaItem{}, nil
	}
	items, err := a.svc.ListPublicListingMedia(ctx, ID(listingID))
	if err != nil {
		return nil, mapMediaContractErr(err)
	}
	out := make([]contracts.PublicMediaItem, 0, len(items))
	for _, item := range items {
		out = append(out, contracts.PublicMediaItem{
			AssetID: contracts.ID(item.AssetID),
			Kind:    string(item.Kind),
			Order:   item.Order,
			URL:     item.URL,
			Width:   item.Width,
			Height:  item.Height,
		})
	}
	return out, nil
}

func toAssetRef(a Asset) contracts.AssetRef {
	ref := contracts.AssetRef{
		ID:          contracts.ID(a.ID),
		OwnerUserID: contracts.ID(a.OwnerUserID),
		Status:      string(a.Status),
	}
	if a.ListingID != nil {
		id := contracts.ID(*a.ListingID)
		ref.ListingID = &id
	}
	return ref
}

func mapMediaContractErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) {
		return contracts.ErrZeroID
	}
	if errors.Is(err, errForbidden) {
		return contracts.ErrForbidden
	}
	if errors.Is(err, errNotFound) {
		return contracts.ErrNotFound
	}
	if errors.Is(err, errNotUsable) {
		return contracts.ErrNotUsable
	}
	if errors.Is(err, errAlreadyAttached) {
		return contracts.ErrAlreadyAttached
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, errStoreRequired) || errors.Is(err, errStorageRequired) {
		return contracts.ErrUnavailable
	}
	return err
}
