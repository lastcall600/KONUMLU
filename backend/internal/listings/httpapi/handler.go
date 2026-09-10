package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	eidscontracts "backend/internal/eids/contracts"
	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/listings"
	locationcontracts "backend/internal/location/contracts"
	mdcontracts "backend/internal/masterdata/contracts"
	mediacontracts "backend/internal/media/contracts"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (listings.ID, error)
}

// Handler is the owner listing HTTP adapter. EİDS results come from contracts only.
type Handler struct {
	sessions    sessionResolver
	orch        *listings.DraftOrchestrator
	svc         *listings.Service
	geo         locationcontracts.ListingLocationReader
	publicMedia mediacontracts.PublicListingMedia
	profiles    identitycontracts.PublicProfileResolver
	policy      mdcontracts.EIDSRequirementLookup
	eidsGate    eidscontracts.ListingPublishGate
	eidsOwner   eidscontracts.OwnerVerification
	origins     map[string]struct{}
}

func New(sessions sessionResolver, orch *listings.DraftOrchestrator, svc *listings.Service, allowedOrigins []string, geo locationcontracts.ListingLocationReader, publicMedia mediacontracts.PublicListingMedia, profiles identitycontracts.PublicProfileResolver) (*Handler, error) {
	if sessions == nil {
		return nil, listings.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, listings.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, listings.ErrUnavailable
	}
	return &Handler{sessions: sessions, orch: orch, svc: svc, geo: geo, publicMedia: publicMedia, profiles: profiles, origins: origins}, nil
}

func (h *Handler) SetEIDS(policy mdcontracts.EIDSRequirementLookup, gate eidscontracts.ListingPublishGate, owner eidscontracts.OwnerVerification) {
	if h == nil {
		return
	}
	h.policy = policy
	h.eidsGate = gate
	h.eidsOwner = owner
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/listings", h.createDraft)
	mux.HandleFunc("GET /v1/listings/{listingId}", h.getOwned)
	mux.HandleFunc("PATCH /v1/listings/{listingId}", h.patchDraft)
	mux.HandleFunc("POST /v1/listings/{listingId}/location", h.setLocation)
	mux.HandleFunc("POST /v1/listings/{listingId}/media", h.attachMedia)
	mux.HandleFunc("POST /v1/listings/{listingId}/ready", h.markReady)
	mux.HandleFunc("POST /v1/listings/{listingId}/publish", h.publish)
	mux.HandleFunc("POST /v1/listings/{listingId}/archive", h.archive)
	mux.HandleFunc("POST /v1/listings/{listingId}/eids-verifications", h.startEIDS)
	mux.HandleFunc("GET /v1/listings/{listingId}/eids-verification", h.getEIDS)
	mux.HandleFunc("GET /v1/public/listings/{listingId}", h.getPublic)
}

func (h *Handler) createDraft(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	if h.orch == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if len(req.ModerationState) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	categoryID, err := listings.ParseID(req.CategoryID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	in := listings.CreateListingDraftInput{
		Content: listings.DraftContent{
			CategoryID:            categoryID,
			CategorySchemaVersion: req.CategorySchemaVersion,
			Title:                 req.Title,
			Description:           req.Description,
			PriceAmount:           req.PriceAmount,
			PriceCurrency:         req.PriceCurrency,
			Attributes:            req.Attributes,
		},
	}
	if req.Location != nil {
		loc, err := parseDraftLocation(*req.Location)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		in.Location = &loc
	}
	assetIDs, err := parseIDs(req.MediaAssetIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	in.MediaAssetIDs = assetIDs
	listing, err := h.orch.CreateListingDraft(r.Context(), ownerID, in)
	if err != nil {
		writeListingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toListingDTO(listing))
}

func (h *Handler) getOwned(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	listing, ok := h.loadOwned(w, r, ownerID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toListingDTO(listing))
}

func (h *Handler) patchDraft(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	current, ok := h.loadOwned(w, r, ownerID)
	if !ok {
		return
	}
	var req patchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if len(req.ModerationState) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if req.CategoryID != "" {
		parsed, err := listings.ParseID(req.CategoryID)
		if err != nil || parsed != current.CategoryID {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	if req.CategorySchemaVersion != nil && *req.CategorySchemaVersion != current.CategorySchemaVersion {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	expected, err := parseUpdatedAt(req.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	content := listings.DraftContent{
		CategoryID:            current.CategoryID,
		CategorySchemaVersion: current.CategorySchemaVersion,
		Title:                 current.Title,
		Description:           current.Description,
		PriceAmount:           current.PriceAmount,
		PriceCurrency:         current.PriceCurrency,
		Attributes:            current.Attributes,
	}
	if req.Title != nil {
		content.Title = *req.Title
	}
	if req.Description != nil {
		content.Description = *req.Description
	}
	if req.PriceAmount != nil || req.PriceCurrency != nil {
		content.PriceAmount = req.PriceAmount
		content.PriceCurrency = req.PriceCurrency
	}
	if req.Attributes != nil {
		content.Attributes = *req.Attributes
	}
	listing, err := h.svc.UpdateDraft(r.Context(), current.ID, expected, content)
	if err != nil {
		writeListingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toListingDTO(listing))
}

func (h *Handler) setLocation(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	if h.orch == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return
	}
	var req locationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	loc, err := parseDraftLocation(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	point, err := h.orch.ReplaceOwnedLocation(r.Context(), ownerID, listingID, loc)
	if err != nil {
		writeListingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, locationResponse{
		ListingID:         listings.ID(point.ListingID).String(),
		Latitude:          point.Latitude,
		Longitude:         point.Longitude,
		CatalogLocationID: catalogIDString(point.CatalogLocationID),
	})
}

func (h *Handler) attachMedia(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	if h.orch == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return
	}
	var req attachRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	assetIDs, err := parseIDs(req.AssetIDs)
	if err != nil || len(assetIDs) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	refs, err := h.orch.AttachOwnedMedia(r.Context(), ownerID, listingID, assetIDs)
	if err != nil {
		writeListingError(w, err)
		return
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, listings.ID(ref.ID).String())
	}
	writeJSON(w, http.StatusOK, attachResponse{ListingID: listingID.String(), AssetIDs: out})
}

func (h *Handler) markReady(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	current, ok := h.loadOwned(w, r, ownerID)
	if !ok {
		return
	}
	if r.Body != nil && r.ContentLength != 0 {
		var req mutationRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	listing, err := h.svc.MarkReady(r.Context(), current.ID, current.UpdatedAt)
	if err != nil {
		writeListingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toListingDTO(listing))
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	current, ok := h.loadOwned(w, r, ownerID)
	if !ok {
		return
	}
	if r.Body != nil && r.ContentLength != 0 {
		var req mutationRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	eligible, err := h.publishEligible(r.Context(), current)
	if err != nil {
		writeListingError(w, err)
		return
	}
	listing, err := h.svc.Publish(r.Context(), current.ID, current.UpdatedAt, eligible)
	if err != nil {
		writeListingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toListingDTO(listing))
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	current, ok := h.loadOwned(w, r, ownerID)
	if !ok {
		return
	}
	if r.Body != nil && r.ContentLength != 0 {
		var req mutationRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	listing, err := h.svc.Archive(r.Context(), current.ID, current.UpdatedAt)
	if err != nil {
		writeListingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toListingDTO(listing))
}

func (h *Handler) loadOwned(w http.ResponseWriter, r *http.Request, ownerID listings.ID) (listings.Listing, bool) {
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return listings.Listing{}, false
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return listings.Listing{}, false
	}
	listing, err := h.svc.Get(r.Context(), listingID)
	if err != nil {
		writeListingError(w, err)
		return listings.Listing{}, false
	}
	if listing.OwnerUserID != ownerID {
		writeError(w, http.StatusNotFound, "not_found")
		return listings.Listing{}, false
	}
	return listing, true
}

func parseListingID(w http.ResponseWriter, r *http.Request) (listings.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("listingId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return listings.ID{}, false
	}
	id, err := listings.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return listings.ID{}, false
	}
	return id, true
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (listings.ID, bool) {
	if !h.requireOrigin(w, r) {
		return listings.ID{}, false
	}
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return listings.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return listings.ID{}, false
	}
	return ownerID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (listings.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return listings.ID{}, false
	}
	ownerID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		writeSessionError(w, err)
		return listings.ID{}, false
	}
	if ownerID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return listings.ID{}, false
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
	CategoryID            string              `json:"categoryId"`
	CategorySchemaVersion int64               `json:"categorySchemaVersion"`
	Title                 string              `json:"title"`
	Description           string              `json:"description"`
	PriceAmount           *string             `json:"priceAmount"`
	PriceCurrency         *string             `json:"priceCurrency"`
	Attributes            listings.Attributes `json:"attributes"`
	Location              *locationRequest    `json:"location"`
	MediaAssetIDs         []string            `json:"mediaAssetIds"`
	OwnerUserID           string              `json:"ownerUserId"`
	Status                string              `json:"status"`
	ModerationState       json.RawMessage     `json:"moderationState"`
}

type patchRequest struct {
	Title                 *string              `json:"title"`
	Description           *string              `json:"description"`
	PriceAmount           *string              `json:"priceAmount"`
	PriceCurrency         *string              `json:"priceCurrency"`
	Attributes            *listings.Attributes `json:"attributes"`
	UpdatedAt             string               `json:"updatedAt"`
	OwnerUserID           string               `json:"ownerUserId"`
	Status                string               `json:"status"`
	CategoryID            string               `json:"categoryId"`
	CategorySchemaVersion *int64               `json:"categorySchemaVersion"`
	ModerationState       json.RawMessage      `json:"moderationState"`
}

type locationRequest struct {
	Latitude          float64 `json:"latitude"`
	Longitude         float64 `json:"longitude"`
	CatalogLocationID string  `json:"catalogLocationId"`
}

type attachRequest struct {
	AssetIDs  []string `json:"assetIds"`
	ObjectKey string   `json:"objectKey"`
}

type mutationRequest struct {
	OwnerUserID string `json:"ownerUserId"`
	Status      string `json:"status"`
}

type listingDTO struct {
	ListingID             string              `json:"listingId"`
	OwnerUserID           string              `json:"ownerUserId"`
	Status                string              `json:"status"`
	ModerationState       string              `json:"moderationState"`
	CategoryID            string              `json:"categoryId"`
	CategorySchemaVersion int64               `json:"categorySchemaVersion"`
	Title                 string              `json:"title"`
	Description           string              `json:"description"`
	PriceAmount           *string             `json:"priceAmount,omitempty"`
	PriceCurrency         *string             `json:"priceCurrency,omitempty"`
	Attributes            listings.Attributes `json:"attributes"`
	CreatedAt             string              `json:"createdAt"`
	UpdatedAt             string              `json:"updatedAt"`
	PublishedAt           *string             `json:"publishedAt,omitempty"`
	ArchivedAt            *string             `json:"archivedAt,omitempty"`
}

type locationResponse struct {
	ListingID         string  `json:"listingId"`
	Latitude          float64 `json:"latitude"`
	Longitude         float64 `json:"longitude"`
	CatalogLocationID string  `json:"catalogLocationId,omitempty"`
}

type attachResponse struct {
	ListingID string   `json:"listingId"`
	AssetIDs  []string `json:"assetIds"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toListingDTO(l listings.Listing) listingDTO {
	return listingDTO{
		ListingID:             l.ID.String(),
		OwnerUserID:           l.OwnerUserID.String(),
		Status:                string(l.Status),
		ModerationState:       string(l.ModerationState.Normalized()),
		CategoryID:            l.CategoryID.String(),
		CategorySchemaVersion: l.CategorySchemaVersion,
		Title:                 l.Title,
		Description:           l.Description,
		PriceAmount:           l.PriceAmount,
		PriceCurrency:         l.PriceCurrency,
		Attributes:            l.Attributes,
		CreatedAt:             l.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:             l.UpdatedAt.UTC().Format(time.RFC3339),
		PublishedAt:           formatTimePtr(l.PublishedAt),
		ArchivedAt:            formatTimePtr(l.ArchivedAt),
	}
}

func writeListingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, listings.ErrInvalidListing), errors.Is(err, listings.ErrInvalidContent),
		errors.Is(err, listings.ErrInvalidPrice), errors.Is(err, listings.ErrInvalidAttributes),
		errors.Is(err, listings.ErrInvalidCategorySchema),
		errors.Is(err, listings.ErrInvalidStatus), errors.Is(err, listings.ErrZeroID),
		errors.Is(err, locationcontracts.ErrInvalidLatitude), errors.Is(err, locationcontracts.ErrInvalidLongitude),
		errors.Is(err, locationcontracts.ErrInvalidCoordinate), errors.Is(err, locationcontracts.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, listings.ErrNotFound), errors.Is(err, listings.ErrForbidden),
		errors.Is(err, mediacontracts.ErrNotFound), errors.Is(err, mediacontracts.ErrForbidden):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, listings.ErrConflict), errors.Is(err, listings.ErrInvalidTransition),
		errors.Is(err, listings.ErrPublishNotEligible),
		errors.Is(err, mediacontracts.ErrNotUsable), errors.Is(err, mediacontracts.ErrAlreadyAttached):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, listings.ErrUnavailable), errors.Is(err, listings.ErrStoreRequired),
		errors.Is(err, mediacontracts.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "unavailable")
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

func decodeJSON(r *http.Request, dest any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	return nil
}

func parseDraftLocation(req locationRequest) (listings.DraftLocation, error) {
	loc := listings.DraftLocation{Latitude: req.Latitude, Longitude: req.Longitude}
	raw := strings.TrimSpace(req.CatalogLocationID)
	if raw == "" {
		return loc, nil
	}
	id, err := listings.ParseID(raw)
	if err != nil {
		return listings.DraftLocation{}, err
	}
	loc.CatalogLocationID = &id
	return loc, nil
}

func parseIDs(raw []string) ([]listings.ID, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]listings.ID, 0, len(raw))
	for _, s := range raw {
		id, err := listings.ParseID(s)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func parseUpdatedAt(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, listings.ErrInvalidListing
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts.UTC(), nil
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return ts.UTC(), nil
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func catalogIDString(id *locationcontracts.ID) string {
	if id == nil {
		return ""
	}
	return listings.ID(*id).String()
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
