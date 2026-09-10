package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/listings/contracts"
	"backend/internal/media"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (media.ID, error)
}

// Handler is the listing-image upload HTTP adapter. It does not process image bytes.
type Handler struct {
	sessions sessionResolver
	svc      *media.Service
	listings contracts.Ownership
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *media.Service, listings contracts.Ownership, allowedOrigins []string) (*Handler, error) {
	if sessions == nil {
		return nil, media.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, media.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, media.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, listings: listings, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/media/listing-images", h.createListingImage)
	mux.HandleFunc("POST /v1/media/listing-images/{assetId}/confirm", h.confirmListingImage)
	mux.HandleFunc("GET /v1/media/listing-images/{assetId}", h.getListingImage)
}

func (h *Handler) createListingImage(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	var req createRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	var filename *string
	if strings.TrimSpace(req.OriginalFilename) != "" {
		name := req.OriginalFilename
		filename = &name
	}
	var listingID *media.ID
	if strings.TrimSpace(req.ListingID) != "" {
		parsed, err := media.ParseID(req.ListingID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		listingID = &parsed
	}
	if listingID != nil {
		if err := h.assertListingOwned(r.Context(), *listingID, ownerID); err != nil {
			writeListingOwnershipError(w, err)
			return
		}
	}
	asset, target, err := h.svc.CreatePending(r.Context(), ownerID, filename)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	if listingID != nil {
		asset, err = h.svc.AttachListing(r.Context(), asset.ID, media.ListingBind{ListingID: *listingID, ActorUserID: ownerID}, asset.UpdatedAt)
		if err != nil {
			writeMediaError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, createResponse{
		AssetID:         asset.ID.String(),
		UploadURL:       target.UploadURL,
		ExpiresAt:       target.ExpiresAt.UTC().Format(time.RFC3339),
		RequiredHeaders: target.RequiredHeaders,
		MaxBytes:        target.MaxBytes,
	})
}

func (h *Handler) confirmListingImage(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	assetID, ok := parseAssetID(w, r)
	if !ok {
		return
	}
	if r.Body != nil && r.ContentLength != 0 {
		var req confirmRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	asset, err := h.svc.ConfirmUpload(r.Context(), assetID, ownerID)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	if asset.Status != media.StatusUploaded {
		writeError(w, http.StatusConflict, "conflict")
		return
	}
	writeJSON(w, http.StatusOK, confirmResponse{
		AssetID: asset.ID.String(),
		Status:  string(media.StatusUploaded),
	})
}

func (h *Handler) getListingImage(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	assetID, ok := parseAssetID(w, r)
	if !ok {
		return
	}
	asset, err := h.svc.Get(r.Context(), assetID, ownerID)
	if err != nil {
		writeMediaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, metadataResponse{
		AssetID:          asset.ID.String(),
		Status:           string(asset.Status),
		Kind:             string(asset.Kind),
		ListingID:        idString(asset.ListingID),
		OriginalFilename: derefString(asset.OriginalFilename),
	})
}

func (h *Handler) assertListingOwned(ctx context.Context, listingID, ownerID media.ID) error {
	if h.listings == nil {
		return media.ErrUnavailable
	}
	return h.listings.AssertListingOwnedBy(ctx, contracts.ID(listingID), contracts.ID(ownerID))
}

func parseAssetID(w http.ResponseWriter, r *http.Request) (media.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("assetId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return media.ID{}, false
	}
	id, err := media.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return media.ID{}, false
	}
	return id, true
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (media.ID, bool) {
	if !h.requireOrigin(w, r) {
		return media.ID{}, false
	}
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return media.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return media.ID{}, false
	}
	return ownerID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (media.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return media.ID{}, false
	}
	ownerID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		writeSessionError(w, err)
		return media.ID{}, false
	}
	if ownerID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return media.ID{}, false
	}
	return ownerID, true
}

func (h *Handler) requireOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || origin == "null" {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if _, ok := h.origins[origin]; !ok {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

type createRequest struct {
	OriginalFilename string `json:"originalFilename"`
	ListingID        string `json:"listingId"`
	OwnerUserID      string `json:"ownerUserId"`
	ObjectKey        string `json:"objectKey"`
}

type confirmRequest struct {
	ObjectKey   string `json:"objectKey"`
	ContentType string `json:"contentType"`
}

type createResponse struct {
	AssetID         string            `json:"assetId"`
	UploadURL       string            `json:"uploadUrl"`
	ExpiresAt       string            `json:"expiresAt"`
	RequiredHeaders map[string]string `json:"requiredHeaders,omitempty"`
	MaxBytes        int64             `json:"maxBytes"`
}

type confirmResponse struct {
	AssetID string `json:"assetId"`
	Status  string `json:"status"`
}

type metadataResponse struct {
	AssetID          string `json:"assetId"`
	Status           string `json:"status"`
	Kind             string `json:"kind"`
	ListingID        string `json:"listingId,omitempty"`
	OriginalFilename string `json:"originalFilename,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeMediaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, media.ErrInvalidFilename), errors.Is(err, media.ErrInvalidAsset),
		errors.Is(err, media.ErrZeroID), errors.Is(err, media.ErrObjectMissing),
		errors.Is(err, media.ErrObjectTooLarge), errors.Is(err, media.ErrInvalidObjectKey):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, media.ErrForbidden), errors.Is(err, media.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, media.ErrConflict), errors.Is(err, media.ErrInvalidTransition),
		errors.Is(err, media.ErrAlreadyAttached):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, media.ErrUnavailable), errors.Is(err, media.ErrStoreRequired),
		errors.Is(err, media.ErrStorageRequired):
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func writeListingOwnershipError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, contracts.ErrNotFound), errors.Is(err, contracts.ErrForbidden):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, contracts.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func writeSessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrUnauthenticated) {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "unavailable")
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorResponse{Error: code})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func readCookie(r *http.Request, name string) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil || c == nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

func csrfOK(r *http.Request) bool {
	cookie, ok := readCookie(r, csrfCookieName)
	if !ok {
		return false
	}
	header := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	if header == "" || len(header) != len(cookie) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(cookie)) == 1
}

func idString(id *media.ID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
