package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/listings"
	locationcontracts "backend/internal/location/contracts"
	mediacontracts "backend/internal/media/contracts"
	staffauth "backend/internal/staffauth/contracts"
)

type staffAuthorizer interface {
	Authenticate(*http.Request) (staffauth.Principal, error)
	Allows(staffauth.Principal, staffauth.Permission) bool
}

// StaffHandler is the read-only staff listing HTTP adapter.
type StaffHandler struct {
	staff    staffAuthorizer
	svc      *listings.Service
	geo      locationcontracts.ListingLocationReader
	media    mediacontracts.PublicListingMedia
	profiles identitycontracts.StaffProfileReader
}

func NewStaff(staff staffAuthorizer, svc *listings.Service, geo locationcontracts.ListingLocationReader, media mediacontracts.PublicListingMedia, profiles identitycontracts.StaffProfileReader) (*StaffHandler, error) {
	if staff == nil || svc == nil {
		return nil, listings.ErrUnavailable
	}
	return &StaffHandler{staff: staff, svc: svc, geo: geo, media: media, profiles: profiles}, nil
}

func (h *StaffHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/staff/listings/{listingId}", h.get)
	mux.HandleFunc("GET /v1/staff/listings", h.list)
}

func (h *StaffHandler) requireStaff(w http.ResponseWriter, r *http.Request, perm staffauth.Permission) bool {
	principal, err := h.staff.Authenticate(r)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) || errors.Is(err, staffauth.ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return false
		}
		if errors.Is(err, staffauth.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return false
	}
	if principal.StaffID.IsZero() {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if !h.staff.Allows(principal, perm) {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func (h *StaffHandler) get(w http.ResponseWriter, r *http.Request) {
	if !h.requireStaff(w, r, staffauth.PermListingsRead) {
		return
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return
	}
	listing, err := h.svc.Get(r.Context(), listingID)
	if err != nil {
		writeListingError(w, err)
		return
	}
	dto, err := h.toStaffListingDTO(r.Context(), listing)
	if err != nil {
		writeListingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (h *StaffHandler) list(w http.ResponseWriter, r *http.Request) {
	if !h.requireStaff(w, r, staffauth.PermListingsRead) {
		return
	}
	rawOwner := strings.TrimSpace(r.URL.Query().Get("ownerPublicProfileId"))
	if rawOwner == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if h.profiles == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	publicID, err := parseProfileID(rawOwner)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ownerUserID, err := h.profiles.StaffUserIDByPublicID(r.Context(), publicID)
	if err != nil {
		writeStaffProfileError(w, err)
		return
	}
	rows, err := h.svc.ListByOwner(r.Context(), listings.ID(ownerUserID))
	if err != nil {
		writeListingError(w, err)
		return
	}
	out := make([]staffListingSummaryDTO, 0, len(rows))
	owner := h.ownerDTO(r.Context(), listings.ID(ownerUserID))
	for _, row := range rows {
		out = append(out, toStaffListingSummaryDTO(row, owner))
	}
	writeJSON(w, http.StatusOK, staffListingListDTO{Listings: out})
}

func (h *StaffHandler) toStaffListingDTO(ctx context.Context, listing listings.Listing) (staffListingDTO, error) {
	loc, err := h.staffLocation(ctx, listing.ID)
	if err != nil {
		return staffListingDTO{}, err
	}
	media, err := h.staffMedia(ctx, listing.ID)
	if err != nil {
		return staffListingDTO{}, err
	}
	return staffListingDTO{
		ListingID:             listing.ID.String(),
		Title:                 listing.Title,
		Description:           listing.Description,
		CategoryID:            listing.CategoryID.String(),
		CategorySchemaVersion: listing.CategorySchemaVersion,
		Status:                string(listing.Status),
		ModerationState:       string(listing.ModerationState.Normalized()),
		CreatedAt:             listing.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:             listing.UpdatedAt.UTC().Format(time.RFC3339),
		PublishedAt:           formatTimePtr(listing.PublishedAt),
		ArchivedAt:            formatTimePtr(listing.ArchivedAt),
		Owner:                 h.ownerDTO(ctx, listing.OwnerUserID),
		Location:              loc,
		Media:                 media,
	}, nil
}

func (h *StaffHandler) ownerDTO(ctx context.Context, ownerUserID listings.ID) *staffListingOwnerDTO {
	if h == nil || h.profiles == nil || ownerUserID.IsZero() {
		return nil
	}
	profile, err := h.profiles.StaffByUserID(ctx, identitycontracts.ID(ownerUserID))
	if err != nil || profile.PublicProfileID.IsZero() {
		return nil
	}
	publicID := listings.ID(profile.PublicProfileID)
	if publicID == ownerUserID {
		return nil
	}
	return &staffListingOwnerDTO{
		PublicProfileID: publicID.String(),
		DisplayName:     cloneDisplayName(profile.DisplayName),
	}
}

func (h *StaffHandler) staffLocation(ctx context.Context, listingID listings.ID) (*publicLocationDTO, error) {
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

func (h *StaffHandler) staffMedia(ctx context.Context, listingID listings.ID) ([]staffMediaDTO, error) {
	if h.media == nil {
		return []staffMediaDTO{}, nil
	}
	items, err := h.media.ListPublicListingMedia(ctx, mediacontracts.ID(listingID))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, listings.ErrUnavailable
	}
	out := make([]staffMediaDTO, 0, len(items))
	for _, item := range items {
		if item.AssetID.IsZero() {
			continue
		}
		out = append(out, staffMediaDTO{
			AssetID: listings.ID(item.AssetID).String(),
			Order:   item.Order,
			Width:   item.Width,
			Height:  item.Height,
		})
	}
	return out, nil
}

func parseProfileID(raw string) (identitycontracts.ID, error) {
	id, err := listings.ParseID(raw)
	if err != nil {
		return identitycontracts.ID{}, err
	}
	return identitycontracts.ID(id), nil
}

func writeStaffProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identitycontracts.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, identitycontracts.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	}
}

type staffListingOwnerDTO struct {
	PublicProfileID string  `json:"publicProfileId"`
	DisplayName     *string `json:"displayName"`
}

type staffMediaDTO struct {
	AssetID string `json:"assetId"`
	Order   int    `json:"order"`
	Width   *int   `json:"width,omitempty"`
	Height  *int   `json:"height,omitempty"`
}

type staffListingDTO struct {
	ListingID             string                `json:"listingId"`
	Title                 string                `json:"title"`
	Description           string                `json:"description"`
	CategoryID            string                `json:"categoryId"`
	CategorySchemaVersion int64                 `json:"categorySchemaVersion"`
	Status                string                `json:"status"`
	ModerationState       string                `json:"moderationState"`
	CreatedAt             string                `json:"createdAt"`
	UpdatedAt             string                `json:"updatedAt"`
	PublishedAt           *string               `json:"publishedAt,omitempty"`
	ArchivedAt            *string               `json:"archivedAt,omitempty"`
	Owner                 *staffListingOwnerDTO `json:"owner,omitempty"`
	Location              *publicLocationDTO    `json:"location"`
	Media                 []staffMediaDTO       `json:"media"`
}

type staffListingSummaryDTO struct {
	ListingID       string                `json:"listingId"`
	Title           string                `json:"title"`
	CategoryID      string                `json:"categoryId"`
	Status          string                `json:"status"`
	ModerationState string                `json:"moderationState"`
	CreatedAt       string                `json:"createdAt"`
	UpdatedAt       string                `json:"updatedAt"`
	Owner           *staffListingOwnerDTO `json:"owner,omitempty"`
}

type staffListingListDTO struct {
	Listings []staffListingSummaryDTO `json:"listings"`
}

func toStaffListingSummaryDTO(listing listings.Listing, owner *staffListingOwnerDTO) staffListingSummaryDTO {
	return staffListingSummaryDTO{
		ListingID:       listing.ID.String(),
		Title:           listing.Title,
		CategoryID:      listing.CategoryID.String(),
		Status:          string(listing.Status),
		ModerationState: string(listing.ModerationState.Normalized()),
		CreatedAt:       listing.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       listing.UpdatedAt.UTC().Format(time.RFC3339),
		Owner:           owner,
	}
}
