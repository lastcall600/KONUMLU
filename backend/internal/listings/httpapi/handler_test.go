package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	eidscontracts "backend/internal/eids/contracts"
	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/listings"
	"backend/internal/location"
	"backend/internal/masterdata/contracts"
	"backend/internal/media"
	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)

	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, validCreateBody(t), map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")

	rec = do(t, h, http.MethodPost, "/v1/listings", "", validCreateBody(t), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")

	rec = do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, validCreateBody(t), authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")
}

func TestCreateOwnerDerivedFromSessionNotClient(t *testing.T) {
	h := newTestHandler(t)
	clientOwner := mustID(t)
	body := validCreateBody(t)
	body["ownerUserId"] = clientOwner.String()
	body["owner_user_id"] = clientOwner.String()
	body["status"] = "published"

	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto listingDTO
	decode(t, rec, &dto)
	if dto.OwnerUserID != h.sessions.userID.String() {
		t.Fatalf("owner = %s want session %s", dto.OwnerUserID, h.sessions.userID)
	}
	if dto.Status != string(listings.StatusDraft) {
		t.Fatalf("status = %s", dto.Status)
	}
	if dto.ModerationState != string(listings.ModerationNone) {
		t.Fatalf("moderationState = %s", dto.ModerationState)
	}
	stored, err := h.listingStore.Get(context.Background(), mustParseListingID(t, dto.ListingID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.OwnerUserID != h.sessions.userID {
		t.Fatalf("stored owner = %s", stored.OwnerUserID)
	}
	if stored.Status != listings.StatusDraft {
		t.Fatalf("stored status = %s", stored.Status)
	}
}

func TestCreateRejectsClientModerationState(t *testing.T) {
	h := newTestHandler(t)
	body := validCreateBody(t)
	body["moderationState"] = "removed"
	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateWithLocation(t *testing.T) {
	h := newTestHandler(t)
	body := validCreateBody(t)
	body["location"] = map[string]any{"latitude": 36.621, "longitude": 29.116}

	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto listingDTO
	decode(t, rec, &dto)
	loc, err := h.locStore.GetByListingID(context.Background(), location.ID(mustParseListingID(t, dto.ListingID)))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Latitude != 36.621 || loc.Longitude != 29.116 {
		t.Fatalf("loc = %+v", loc)
	}
}

func TestCreateWithOwnedMedia(t *testing.T) {
	h := newTestHandler(t)
	asset, _, err := h.mediaSvc.CreatePending(context.Background(), media.ID(h.sessions.userID), nil)
	if err != nil {
		t.Fatal(err)
	}
	body := validCreateBody(t)
	body["mediaAssetIds"] = []string{asset.ID.String()}

	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto listingDTO
	decode(t, rec, &dto)
	stored, err := h.mediaStore.Get(context.Background(), asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ListingID == nil || listings.ID(*stored.ListingID) != mustParseListingID(t, dto.ListingID) {
		t.Fatalf("attached = %v", stored.ListingID)
	}
}

func TestCreateRejectsForeignMedia(t *testing.T) {
	h := newTestHandler(t)
	other := mustID(t)
	asset, _, err := h.mediaSvc.CreatePending(context.Background(), media.ID(other), nil)
	if err != nil {
		t.Fatal(err)
	}
	body := validCreateBody(t)
	body["mediaAssetIds"] = []string{asset.ID.String()}

	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
	listed, err := h.listingStore.ListByOwner(context.Background(), h.sessions.userID)
	if err != nil || len(listed) != 0 {
		t.Fatalf("must not create listing: %+v err=%v", listed, err)
	}
}

func TestGetOwnerListing(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)

	rec := do(t, h, http.MethodGet, "/v1/listings/"+created.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto listingDTO
	decode(t, rec, &dto)
	if dto.ListingID != created.ID.String() || dto.Status != string(listings.StatusDraft) {
		t.Fatalf("dto = %+v", dto)
	}
	if dto.Title != created.Title || dto.CategoryID != created.CategoryID.String() {
		t.Fatalf("dto = %+v", dto)
	}
	if dto.CreatedAt == "" || dto.UpdatedAt == "" {
		t.Fatal("timestamps required")
	}
}

func TestForeignGetIsPrivacySafe404(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	h.sessions.userID = mustID(t)

	rec := do(t, h, http.MethodGet, "/v1/listings/"+created.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
	assertNoSensitiveLeak(t, rec.Body.String())
	if strings.Contains(rec.Body.String(), created.Title) {
		t.Fatal("must not leak listing content")
	}
}

func TestPatchDraftSuccess(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)

	rec := do(t, h, http.MethodPatch, "/v1/listings/"+created.ID.String(), allowedOrigin, map[string]any{
		"title":       "Updated bike",
		"updatedAt":   created.UpdatedAt.UTC().Format(time.RFC3339),
		"status":      "published",
		"ownerUserId": mustID(t).String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto listingDTO
	decode(t, rec, &dto)
	if dto.Title != "Updated bike" || dto.Status != string(listings.StatusDraft) {
		t.Fatalf("dto = %+v", dto)
	}
	if dto.OwnerUserID != h.sessions.userID.String() {
		t.Fatalf("owner mutated: %s", dto.OwnerUserID)
	}
	stored, err := h.listingStore.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title != "Updated bike" || stored.Status != listings.StatusDraft {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestPatchNonDraftRejected(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	ready, err := h.svc.MarkReady(context.Background(), created.ID, created.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}

	rec := do(t, h, http.MethodPatch, "/v1/listings/"+ready.ID.String(), allowedOrigin, map[string]any{
		"title":     "Nope",
		"updatedAt": ready.UpdatedAt.UTC().Format(time.RFC3339),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "conflict")
}

func TestPatchRejectsCategoryChange(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	rec := do(t, h, http.MethodPatch, "/v1/listings/"+created.ID.String(), allowedOrigin, map[string]any{
		"categoryId": mustID(t).String(),
		"updatedAt":  created.UpdatedAt.UTC().Format(time.RFC3339),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSetLocationOwnershipEnforced(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	h.sessions.userID = mustID(t)

	rec := do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/location", allowedOrigin, map[string]any{
		"latitude":  36.6,
		"longitude": 29.1,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
	if len(h.locStore.Snapshot()) != 0 {
		t.Fatalf("location written: %+v", h.locStore.Snapshot())
	}
}

func TestSetLocationSuccess(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	rec := do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/location", allowedOrigin, map[string]any{
		"latitude":  36.621,
		"longitude": 29.116,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	loc, err := h.locStore.GetByListingID(context.Background(), location.ID(created.ID))
	if err != nil || loc.Latitude != 36.621 {
		t.Fatalf("loc = %+v err=%v", loc, err)
	}
}

func TestAttachMediaOwnershipEnforced(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	asset, _, err := h.mediaSvc.CreatePending(context.Background(), media.ID(h.sessions.userID), nil)
	if err != nil {
		t.Fatal(err)
	}
	h.sessions.userID = mustID(t)

	rec := do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/media", allowedOrigin, map[string]any{
		"assetIds":  []string{asset.ID.String()},
		"objectKey": "media/listing-images/secret",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := h.mediaStore.Get(context.Background(), asset.ID)
	if err != nil || stored.ListingID != nil {
		t.Fatalf("must not attach: %+v err=%v", stored, err)
	}
}

func TestAttachMediaRejectsForeignAsset(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	other := mustID(t)
	asset, _, err := h.mediaSvc.CreatePending(context.Background(), media.ID(other), nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/media", allowedOrigin, map[string]any{
		"assetIds": []string{asset.ID.String()},
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReadyTransition(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	rec := do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/ready", allowedOrigin, map[string]any{
		"status":      "published",
		"ownerUserId": mustID(t).String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto listingDTO
	decode(t, rec, &dto)
	if dto.Status != string(listings.StatusReady) {
		t.Fatalf("status = %s", dto.Status)
	}
	if dto.OwnerUserID != h.sessions.userID.String() {
		t.Fatalf("owner = %s", dto.OwnerUserID)
	}
	stored, err := h.listingStore.Get(context.Background(), created.ID)
	if err != nil || stored.Status != listings.StatusReady {
		t.Fatalf("stored = %+v err=%v", stored, err)
	}
}

func TestArchive(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	rec := do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/archive", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto listingDTO
	decode(t, rec, &dto)
	if dto.Status != string(listings.StatusArchived) || dto.ArchivedAt == nil {
		t.Fatalf("dto = %+v", dto)
	}
}

func TestOwnerPublishSuccess(t *testing.T) {
	h := newTestHandler(t)
	ready := markReady(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodPost, "/v1/listings/"+ready.ID.String()+"/publish", allowedOrigin, map[string]any{
		"eligible":    false,
		"status":      "draft",
		"ownerUserId": mustID(t).String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto listingDTO
	decode(t, rec, &dto)
	if dto.Status != string(listings.StatusPublished) || dto.PublishedAt == nil {
		t.Fatalf("dto = %+v", dto)
	}
	if dto.OwnerUserID != h.sessions.userID.String() {
		t.Fatalf("owner = %s", dto.OwnerUserID)
	}
	stored, err := h.listingStore.Get(context.Background(), ready.ID)
	if err != nil || stored.Status != listings.StatusPublished {
		t.Fatalf("stored = %+v err=%v", stored, err)
	}
}

func TestForeignPublishIsPrivacySafe404(t *testing.T) {
	h := newTestHandler(t)
	ready := markReady(t, h, createDraft(t, h))
	h.sessions.userID = mustID(t)
	rec := do(t, h, http.MethodPost, "/v1/listings/"+ready.ID.String()+"/publish", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
	assertNoSensitiveLeak(t, rec.Body.String())
	stored, err := h.listingStore.Get(context.Background(), ready.ID)
	if err != nil || stored.Status != listings.StatusReady {
		t.Fatalf("must not publish: %+v err=%v", stored, err)
	}
}

func TestPublishInvalidLifecycleRejected(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	rec := do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/publish", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "conflict")
	stored, err := h.listingStore.Get(context.Background(), created.ID)
	if err != nil || stored.Status != listings.StatusDraft {
		t.Fatalf("stored = %+v err=%v", stored, err)
	}
}

func TestPublishEmitsListingPublishedEvent(t *testing.T) {
	h := newTestHandler(t)
	ready := markReady(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodPost, "/v1/listings/"+ready.ID.String()+"/publish", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(h.outbox.events) != 1 || h.outbox.events[0].EventType != "listings.listing.published" {
		t.Fatalf("events = %+v", h.outbox.events)
	}
}

func TestInfrastructureIs503WithoutInternalDetail(t *testing.T) {
	h := newTestHandler(t)
	h.listingStore.SetFail(db.ErrUnavailable)
	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, validCreateBody(t), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())
	if strings.Contains(rec.Body.String(), "postgres") || strings.Contains(rec.Body.String(), "sql") {
		t.Fatal("must not leak db details")
	}
}

func TestMalformedJSONAndIDAre400(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, map[string]any{
		"categoryId": "not-a-uuid",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/v1/listings/not-a-uuid", "", nil, authedCookies())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("get status = %d", rec.Code)
	}
}

func TestInvalidSchemaAndAttributesAre400(t *testing.T) {
	h := newTestHandler(t)
	h.forms.err = contracts.ErrNotFound
	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, validCreateBody(t), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unpublished status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")

	h.forms.err = nil
	body := validCreateBody(t)
	body["attributes"] = map[string]any{"condition": "used", "unknown": "x"}
	rec = do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown attr status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "bad_request")
}

func TestPatchValidatesOriginalSchemaVersion(t *testing.T) {
	h := newTestHandler(t)
	h.forms.byVersion = map[int64][]contracts.PublishedField{
		1: {{Code: "condition", ValueType: contracts.ValueTypeEnum, Required: true, EnumOptionCodes: []string{"used"}}},
		2: {{Code: "year", ValueType: contracts.ValueTypeInteger, Required: true}},
	}
	created := createDraft(t, h)
	rec := do(t, h, http.MethodPatch, "/v1/listings/"+created.ID.String(), allowedOrigin, map[string]any{
		"attributes":            map[string]any{"condition": "used"},
		"updatedAt":             created.UpdatedAt.UTC().Format(time.RFC3339),
		"categorySchemaVersion": 2,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("schema switch status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPatch, "/v1/listings/"+created.ID.String(), allowedOrigin, map[string]any{
		"attributes": map[string]any{"condition": "used"},
		"updatedAt":  created.UpdatedAt.UTC().Format(time.RFC3339),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch original version status = %d body=%s", rec.Code, rec.Body.String())
	}
	if h.forms.lastVersion != 1 {
		t.Fatalf("resolved version = %d", h.forms.lastVersion)
	}
}

func TestMasterDataUnavailableIs503(t *testing.T) {
	h := newTestHandler(t)
	h.forms.err = contracts.ErrUnavailable
	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, validCreateBody(t), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())
}

type testHandler struct {
	*Handler
	sessions     *fakeSessions
	listingStore *listings.MemoryStore
	locStore     *location.MemoryStore
	mediaStore   *media.MemoryStore
	mediaObjects *media.MemoryObjectStorage
	mediaSvc     *media.Service
	forms        *httpPublishedForms
	outbox       *memoryEnqueuer
}

type httpPublishedForms struct {
	fields      []contracts.PublishedField
	byVersion   map[int64][]contracts.PublishedField
	err         error
	lastVersion int64
}

func defaultHTTPForms() *httpPublishedForms {
	return &httpPublishedForms{
		fields: []contracts.PublishedField{{
			Code:            "condition",
			ValueType:       contracts.ValueTypeEnum,
			Required:        true,
			EnumOptionCodes: []string{"used"},
		}},
	}
}

type noneEIDSPolicy struct{}

func (noneEIDSPolicy) Requirement(context.Context, contracts.ID) (contracts.EIDSRequirement, error) {
	return contracts.EIDSRequirementNone, nil
}

type staticEIDSPolicy struct {
	req contracts.EIDSRequirement
	err error
}

func (s staticEIDSPolicy) Requirement(context.Context, contracts.ID) (contracts.EIDSRequirement, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.req, nil
}

type staticEIDSGate struct {
	verified bool
	err      error
}

func (s staticEIDSGate) IsVerified(context.Context, eidscontracts.ID, eidscontracts.VerificationType) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.verified, nil
}

func (s *httpPublishedForms) ResolvePublishedForm(_ context.Context, categoryID contracts.ID, schemaVersion int64) (contracts.PublishedForm, error) {
	s.lastVersion = schemaVersion
	if s.err != nil {
		return contracts.PublishedForm{}, s.err
	}
	fields := s.fields
	if s.byVersion != nil {
		f, ok := s.byVersion[schemaVersion]
		if !ok {
			return contracts.PublishedForm{}, contracts.ErrNotFound
		}
		fields = f
	}
	return contracts.PublishedForm{
		CategoryID:    categoryID,
		SchemaVersion: schemaVersion,
		Fields:        fields,
	}, nil
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	listingStore := listings.NewMemoryStore()
	locStore := location.NewMemoryStore()
	mediaStore := media.NewMemoryStore()
	mediaObjects := media.NewMemoryObjectStorage()
	clock := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	forms := defaultHTTPForms()
	listingSvc, err := listings.NewService(listingStore, forms, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	enq := &memoryEnqueuer{}
	listingSvc.SetOutbox(nil, enq)
	locSvc, err := location.NewService(locStore, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	mediaSvc, err := media.NewService(mediaStore, mediaObjects, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	orch, err := listings.NewDraftOrchestrator(listingSvc, location.NewGeo(locSvc), media.NewListingMedia(mediaSvc), nil)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{userID: mustID(t)}
	h, err := New(sessions, orch, listingSvc, []string{allowedOrigin}, location.NewListingLocations(locSvc), media.NewPublicListingMedia(mediaSvc), nil)
	if err != nil {
		t.Fatal(err)
	}
	h.SetEIDS(noneEIDSPolicy{}, nil, nil)
	return &testHandler{
		Handler:      h,
		sessions:     sessions,
		listingStore: listingStore,
		locStore:     locStore,
		mediaStore:   mediaStore,
		mediaObjects: mediaObjects,
		mediaSvc:     mediaSvc,
		forms:        forms,
		outbox:       enq,
	}
}

func createDraft(t *testing.T, h *testHandler) listings.Listing {
	t.Helper()
	listing, err := h.svc.CreateDraft(context.Background(), h.sessions.userID, listings.DraftContent{
		CategoryID:            mustID(t),
		CategorySchemaVersion: 1,
		Title:                 "Bike",
		Description:           "Used bicycle",
		Attributes:            listings.Attributes{"condition": "used"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return listing
}

func markReady(t *testing.T, h *testHandler, listing listings.Listing) listings.Listing {
	t.Helper()
	ready, err := h.svc.MarkReady(context.Background(), listing.ID, listing.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return ready
}

func validCreateBody(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"categoryId":            mustID(t).String(),
		"categorySchemaVersion": 1,
		"title":                 "Bike",
		"description":           "Used bicycle",
		"attributes":            map[string]any{"condition": "used"},
	}
}

type fakeSessions struct {
	userID listings.ID
	err    error
}

func (f *fakeSessions) Resolve(context.Context, string) (listings.ID, error) {
	if f.err != nil {
		return listings.ID{}, f.err
	}
	return f.userID, nil
}

type fakeProfiles struct {
	profile identitycontracts.PublicProfile
	err     error
	calls   int
}

func (f *fakeProfiles) ResolveByPublicID(context.Context, identitycontracts.ID) (identitycontracts.PublicProfile, error) {
	return identitycontracts.PublicProfile{}, identitycontracts.ErrUnavailable
}

func (f *fakeProfiles) ResolveByUserID(_ context.Context, _ identitycontracts.ID) (identitycontracts.PublicProfile, error) {
	f.calls++
	if f.err != nil {
		return identitycontracts.PublicProfile{}, f.err
	}
	return f.profile, nil
}

func (f *fakeProfiles) ResolveUserIDByPublicID(context.Context, identitycontracts.ID) (identitycontracts.ID, error) {
	return identitycontracts.ID{}, identitycontracts.ErrUnavailable
}

type memoryEnqueuer struct {
	events []outbox.NewEvent
}

func (e *memoryEnqueuer) Enqueue(_ context.Context, _ outbox.Execer, in outbox.NewEvent) (outbox.Event, error) {
	e.events = append(e.events, in)
	id, err := outbox.NewID()
	if err != nil {
		return outbox.Event{}, err
	}
	return outbox.Event{ID: id, EventType: in.EventType, EventVersion: in.EventVersion, Payload: in.Payload}, nil
}

func authedCookies() map[string]string {
	return map[string]string{
		sessionCookieName: "session-raw-token",
		csrfCookieName:    "csrf-token",
	}
}

type headerOption func(*http.Request)

func withCSRF(token string) headerOption {
	return func(r *http.Request) {
		r.Header.Set(csrfHeaderName, token)
	}
}

func do(t *testing.T, h interface{ Register(*http.ServeMux) }, method, path, origin string, body any, cookies map[string]string, opts ...headerOption) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for name, value := range cookies {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	for _, opt := range opts {
		opt(r)
	}
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(rec, r)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dest); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body errorResponse
	decode(t, rec, &body)
	if body.Error != code {
		t.Fatalf("error = %q want %q", body.Error, code)
	}
}

func assertNoSensitiveLeak(t *testing.T, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	banned := []string{"postgres", "sql:", "minio", "s3", "stack", "panic", "secret"}
	for _, w := range banned {
		if strings.Contains(lower, w) {
			t.Fatalf("sensitive token %q leaked in %q", w, body)
		}
	}
}

func mustID(t *testing.T) listings.ID {
	t.Helper()
	id, err := listings.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustParseListingID(t *testing.T, raw string) listings.ID {
	t.Helper()
	id, err := listings.ParseID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSessionInfraIs503(t *testing.T) {
	h := newTestHandler(t)
	h.sessions.err = errors.New("valkey timeout")
	rec := do(t, h, http.MethodPost, "/v1/listings", allowedOrigin, validCreateBody(t), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
}
