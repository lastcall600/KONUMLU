package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/listings"
	locationcontracts "backend/internal/location/contracts"
	mediacontracts "backend/internal/media/contracts"
)

const publicListingCacheControl = "public, max-age=30"

type publicListingDTO struct {
	ListingID             string              `json:"listingId"`
	CategoryID            string              `json:"categoryId"`
	CategorySchemaVersion int64               `json:"categorySchemaVersion"`
	Title                 string              `json:"title"`
	Description           string              `json:"description"`
	PriceAmount           *string             `json:"priceAmount"`
	PriceCurrency         *string             `json:"priceCurrency"`
	Attributes            listings.Attributes `json:"attributes"`
	Location              *publicLocationDTO  `json:"location"`
	Media                 []publicMediaDTO    `json:"media"`
	Seller                *publicSellerDTO    `json:"seller,omitempty"`
	PublishedAt           *string             `json:"publishedAt"`
	UpdatedAt             string              `json:"updatedAt"`
}

type publicSellerDTO struct {
	PublicProfileID string  `json:"publicProfileId"`
	DisplayName     *string `json:"displayName"`
}

type publicMediaDTO struct {
	AssetID string `json:"assetId"`
	URL     string `json:"url"`
	Order   int    `json:"order"`
	Width   *int   `json:"width,omitempty"`
	Height  *int   `json:"height,omitempty"`
}

type publicLocationDTO struct {
	Latitude          float64 `json:"latitude"`
	Longitude         float64 `json:"longitude"`
	CatalogLocationID *string `json:"catalogLocationId"`
}

func (h *Handler) getPublic(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return
	}
	listing, err := h.svc.GetPublished(r.Context(), listingID)
	if err != nil {
		writeListingError(w, err)
		return
	}
	loc, err := h.publicLocation(r.Context(), listing.ID)
	if err != nil {
		writeListingError(w, err)
		return
	}
	media, err := h.publicListingMedia(r.Context(), listing.ID)
	if err != nil {
		writeListingError(w, err)
		return
	}
	seller := h.publicSeller(r.Context(), listing.OwnerUserID)
	w.Header().Set("Cache-Control", publicListingCacheControl)
	writeJSON(w, http.StatusOK, toPublicListingDTO(listing, loc, media, seller))
}

func (h *Handler) publicLocation(ctx context.Context, listingID listings.ID) (*publicLocationDTO, error) {
	if h.geo == nil {
		return nil, nil
	}
	point, err := h.geo.GetListingLocation(ctx, locationcontracts.ID(listingID))
	if err != nil {
		if errors.Is(err, locationcontracts.ErrNotFound) {
			return nil, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, listings.ErrUnavailable
	}
	dto := &publicLocationDTO{
		Latitude:  point.Latitude,
		Longitude: point.Longitude,
	}
	if point.CatalogLocationID != nil {
		s := listings.ID(*point.CatalogLocationID).String()
		dto.CatalogLocationID = &s
	}
	return dto, nil
}

func (h *Handler) publicListingMedia(ctx context.Context, listingID listings.ID) ([]publicMediaDTO, error) {
	if h.publicMedia == nil {
		return []publicMediaDTO{}, nil
	}
	items, err := h.publicMedia.ListPublicListingMedia(ctx, mediacontracts.ID(listingID))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if errors.Is(err, mediacontracts.ErrZeroID) {
			return nil, listings.ErrZeroID
		}
		if errors.Is(err, mediacontracts.ErrUnavailable) {
			return nil, listings.ErrUnavailable
		}
		return nil, listings.ErrUnavailable
	}
	out := make([]publicMediaDTO, 0, len(items))
	for _, item := range items {
		if item.URL == "" || item.AssetID.IsZero() {
			continue
		}
		dto := publicMediaDTO{
			AssetID: listings.ID(item.AssetID).String(),
			URL:     item.URL,
			Order:   item.Order,
			Width:   item.Width,
			Height:  item.Height,
		}
		out = append(out, dto)
	}
	return out, nil
}

func (h *Handler) publicSeller(ctx context.Context, ownerUserID listings.ID) *publicSellerDTO {
	if h == nil || h.profiles == nil || ownerUserID.IsZero() {
		return nil
	}
	profile, err := h.profiles.ResolveByUserID(ctx, identitycontracts.ID(ownerUserID))
	if err != nil || profile.PublicProfileID.IsZero() {
		return nil
	}
	publicID := listings.ID(profile.PublicProfileID)
	if publicID == ownerUserID {
		return nil
	}
	return &publicSellerDTO{
		PublicProfileID: publicID.String(),
		DisplayName:     cloneDisplayName(profile.DisplayName),
	}
}

func cloneDisplayName(name *string) *string {
	if name == nil {
		return nil
	}
	out := *name
	return &out
}

func toPublicListingDTO(l listings.Listing, loc *publicLocationDTO, media []publicMediaDTO, seller *publicSellerDTO) publicListingDTO {
	attrs := l.Attributes
	if attrs == nil {
		attrs = listings.Attributes{}
	}
	if media == nil {
		media = []publicMediaDTO{}
	}
	return publicListingDTO{
		ListingID:             l.ID.String(),
		CategoryID:            l.CategoryID.String(),
		CategorySchemaVersion: l.CategorySchemaVersion,
		Title:                 l.Title,
		Description:           l.Description,
		PriceAmount:           l.PriceAmount,
		PriceCurrency:         l.PriceCurrency,
		Attributes:            attrs,
		Location:              loc,
		Media:                 media,
		Seller:                seller,
		PublishedAt:           formatTimePtr(l.PublishedAt),
		UpdatedAt:             l.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
