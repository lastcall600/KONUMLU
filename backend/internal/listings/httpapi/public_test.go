package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/listings"
	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/location"
	"backend/internal/media"
	"backend/internal/platform/db"
)

func TestPublicGetPublishedListing(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != publicListingCacheControl {
		t.Fatalf("cache = %q", rec.Header().Get("Cache-Control"))
	}
	var dto publicListingDTO
	decode(t, rec, &dto)
	if dto.ListingID != published.ID.String() {
		t.Fatalf("listingId = %s", dto.ListingID)
	}
	if dto.CategoryID != published.CategoryID.String() || dto.CategorySchemaVersion != published.CategorySchemaVersion {
		t.Fatalf("category = %s v%d", dto.CategoryID, dto.CategorySchemaVersion)
	}
	if dto.Title != published.Title || dto.Description != published.Description {
		t.Fatalf("content = %+v", dto)
	}
	if dto.Location != nil {
		t.Fatalf("location = %+v want null", dto.Location)
	}
	if dto.PublishedAt == nil || *dto.PublishedAt != published.PublishedAt.UTC().Format(time.RFC3339) {
		t.Fatalf("publishedAt = %v", dto.PublishedAt)
	}
	if dto.UpdatedAt != published.UpdatedAt.UTC().Format(time.RFC3339) {
		t.Fatalf("updatedAt = %s", dto.UpdatedAt)
	}
}

func TestPublicGetNonPublicIs404(t *testing.T) {
	h := newTestHandler(t)
	draft := createDraft(t, h)
	ready := markReady(t, h, createDraft(t, h))
	pending := seedStatus(t, h, listings.StatusVerificationPending)
	archived := archiveListing(t, h, publishListing(t, h, createDraft(t, h)))
	restricted := restrictListing(t, h, publishListing(t, h, createDraft(t, h)), listings.ModerationRestricted)
	removed := restrictListing(t, h, publishListing(t, h, createDraft(t, h)), listings.ModerationRemoved)
	missing, err := listings.NewID()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		id   listings.ID
	}{
		{"draft", draft.ID},
		{"ready", ready.ID},
		{"verification_pending", pending.ID},
		{"archived", archived.ID},
		{"restricted", restricted.ID},
		{"removed", removed.ID},
		{"missing", missing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, h, http.MethodGet, "/v1/public/listings/"+tc.id.String(), "", nil, nil)
			body := rec.Body.String()
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d body=%s", rec.Code, body)
			}
			var errBody errorResponse
			if err := json.Unmarshal([]byte(body), &errBody); err != nil {
				t.Fatal(err)
			}
			if errBody.Error != "not_found" {
				t.Fatalf("error = %q", errBody.Error)
			}
			if rec.Header().Get("Cache-Control") == publicListingCacheControl {
				t.Fatal("must not cache non-public responses")
			}
			assertNoSensitiveLeak(t, body)
			lower := strings.ToLower(body)
			for _, leak := range []string{"draft", "ready", "verification", "archived", "owneruserid"} {
				if strings.Contains(lower, leak) {
					t.Fatalf("leaked %q in %s", leak, body)
				}
			}
		})
	}
}

func TestPublicGetRestoredListingIsVisible(t *testing.T) {
	h := newTestHandler(t)
	restricted := restrictListing(t, h, publishListing(t, h, createDraft(t, h)), listings.ModerationRestricted)
	if err := h.svc.ClearModerationState(context.Background(), listingcontracts.ID(restricted.ID)); err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+restricted.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("restored status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPublicGetArchivedStaysHiddenAfterModerationClear(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	removed := restrictListing(t, h, published, listings.ModerationRemoved)
	archived := archiveListing(t, h, removed)
	if err := h.svc.ClearModerationState(context.Background(), listingcontracts.ID(archived.ID)); err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+archived.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("archived restore status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPublicGetMalformedUUIDIs400(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/public/listings/not-a-uuid", "", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
}

func TestPublicDTOExcludesPrivateFields(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]struct{}{
		"listingId":             {},
		"categoryId":            {},
		"categorySchemaVersion": {},
		"title":                 {},
		"description":           {},
		"priceAmount":           {},
		"priceCurrency":         {},
		"attributes":            {},
		"location":              {},
		"media":                 {},
		"seller":                {},
		"publishedAt":           {},
		"updatedAt":             {},
	}
	for k := range raw {
		if _, ok := allowed[k]; !ok {
			t.Fatalf("unexpected public field %q", k)
		}
	}
	banned := []string{
		"ownerUserId", "owner_user_id", "status", "createdAt", "archivedAt",
		"objectKey", "object_key", "processedObjectKey", "processed_object_key", "outbox", "session", "moderation",
	}
	for _, key := range banned {
		if strings.Contains(body, key) {
			t.Fatalf("private field %q leaked in %s", key, body)
		}
	}
}

func TestPublicGetReturnsLocationWhenPresent(t *testing.T) {
	h := newTestHandler(t)
	draft := createDraft(t, h)
	catalogID := mustID(t)
	rec := do(t, h, http.MethodPost, "/v1/listings/"+draft.ID.String()+"/location", allowedOrigin, map[string]any{
		"latitude":          36.621,
		"longitude":         29.116,
		"catalogLocationId": catalogID.String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("set location status = %d body=%s", rec.Code, rec.Body.String())
	}
	published := publishListing(t, h, draft)

	rec = do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto publicListingDTO
	decode(t, rec, &dto)
	if dto.Location == nil {
		t.Fatal("location missing")
	}
	if dto.Location.Latitude != 36.621 || dto.Location.Longitude != 29.116 {
		t.Fatalf("location = %+v", dto.Location)
	}
	if dto.Location.CatalogLocationID == nil || *dto.Location.CatalogLocationID != catalogID.String() {
		t.Fatalf("catalogLocationId = %v", dto.Location.CatalogLocationID)
	}
}

func TestPublicGetLocationNullWhenAbsent(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	decode(t, rec, &raw)
	if raw["location"] != nil {
		t.Fatalf("location = %#v", raw["location"])
	}
}

func TestPublicGetDBUnavailableIs503(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	h.listingStore.SetFail(db.ErrUnavailable)

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())
	if strings.Contains(rec.Body.String(), "sql") || strings.Contains(rec.Body.String(), "postgres") {
		t.Fatalf("internal detail leaked: %s", rec.Body.String())
	}
}

func TestPublicGetLocationStoreUnavailableIs503(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	h.locStore.SetFail(location.ErrUnavailable)

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())
}

func TestPublicGetReadyMediaURL(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	asset := seedReadyAttachedMedia(t, h, published.ID, 0)

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto publicListingDTO
	decode(t, rec, &dto)
	if len(dto.Media) != 1 {
		t.Fatalf("media = %+v", dto.Media)
	}
	item := dto.Media[0]
	if item.AssetID != asset.ID.String() {
		t.Fatalf("assetId = %s", item.AssetID)
	}
	if item.URL == "" || item.Order != 0 {
		t.Fatalf("item = %+v", item)
	}
	if item.Width == nil || item.Height == nil || *item.Width != 10 || *item.Height != 10 {
		t.Fatalf("dims = %+v", item)
	}
	if strings.Contains(rec.Body.String(), asset.ObjectKey) {
		t.Fatal("original object key leaked")
	}
	if strings.Contains(rec.Body.String(), `"objectKey"`) || strings.Contains(rec.Body.String(), "processedObjectKey") {
		t.Fatal("object key field leaked")
	}
}

func TestPublicGetOmitsNonReadyAndUnattachedAndForeignMedia(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	ready := seedReadyAttachedMedia(t, h, published.ID, 1)
	seedNonReadyAttached(t, h, published.ID, media.StatusPendingUpload)
	seedNonReadyAttached(t, h, published.ID, media.StatusUploaded)
	seedNonReadyAttached(t, h, published.ID, media.StatusProcessing)
	seedNonReadyAttached(t, h, published.ID, media.StatusRejected)
	seedDeletedAttached(t, h, published.ID)
	unattached := seedReadyUnattached(t, h)
	other := publishListing(t, h, createDraft(t, h))
	foreign := seedReadyAttachedMedia(t, h, other.ID, 0)

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto publicListingDTO
	decode(t, rec, &dto)
	if len(dto.Media) != 1 || dto.Media[0].AssetID != ready.ID.String() {
		t.Fatalf("media = %+v", dto.Media)
	}
	body := rec.Body.String()
	if strings.Contains(body, unattached.ID.String()) || strings.Contains(body, foreign.ID.String()) {
		t.Fatalf("leaked other asset: %s", body)
	}
	if strings.Contains(body, ready.ObjectKey) || strings.Contains(body, unattached.ObjectKey) || strings.Contains(body, foreign.ObjectKey) {
		t.Fatal("original object key leaked")
	}
}

func TestPublicGetNonPublishedStill404WithMediaPresent(t *testing.T) {
	h := newTestHandler(t)
	draft := createDraft(t, h)
	asset := seedReadyAttachedMedia(t, h, draft.ID, 0)
	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+draft.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, asset.ID.String()) || strings.Contains(body, "objects.test") || strings.Contains(body, asset.ObjectKey) {
		t.Fatalf("media leaked on 404: %s", body)
	}
}

func TestPublicGetMediaEmptyWhenReaderNil(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	_ = seedReadyAttachedMedia(t, h, published.ID, 0)
	h.publicMedia = nil

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	decode(t, rec, &raw)
	media, ok := raw["media"].([]any)
	if !ok || len(media) != 0 {
		t.Fatalf("media = %#v", raw["media"])
	}
}

func TestPublicGetMediaDeliveryUnavailableIs503(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	_ = seedReadyAttachedMedia(t, h, published.ID, 0)
	h.mediaObjects.SetFail(media.ErrUnavailable)

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())
}

func TestPublicGetMediaStoreUnavailableIs503(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	h.mediaStore.SetFail(db.ErrUnavailable)

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())
}

func TestPublicGetIncludesSellerFromResolver(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	publicID := mustID(t)
	name := "Ada"
	h.profiles = &fakeProfiles{profile: identitycontracts.PublicProfile{
		PublicProfileID: identitycontracts.ID(publicID),
		DisplayName:     &name,
	}}

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto publicListingDTO
	decode(t, rec, &dto)
	if dto.Seller == nil {
		t.Fatal("seller missing")
	}
	if dto.Seller.PublicProfileID != publicID.String() {
		t.Fatalf("publicProfileId = %s", dto.Seller.PublicProfileID)
	}
	if dto.Seller.DisplayName == nil || *dto.Seller.DisplayName != name {
		t.Fatalf("displayName = %v", dto.Seller.DisplayName)
	}
	body := rec.Body.String()
	if strings.Contains(body, published.OwnerUserID.String()) {
		t.Fatalf("owner user id leaked: %s", body)
	}
	if strings.Contains(body, `"ownerUserId"`) || strings.Contains(body, `"userId"`) || strings.Contains(body, `"user_id"`) {
		t.Fatalf("owner identity field leaked: %s", body)
	}
}

func TestPublicGetSellerDisplayNameNull(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	publicID := mustID(t)
	h.profiles = &fakeProfiles{profile: identitycontracts.PublicProfile{
		PublicProfileID: identitycontracts.ID(publicID),
	}}

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	decode(t, rec, &raw)
	seller, ok := raw["seller"].(map[string]any)
	if !ok {
		t.Fatalf("seller = %#v", raw["seller"])
	}
	if seller["publicProfileId"] != publicID.String() {
		t.Fatalf("publicProfileId = %#v", seller["publicProfileId"])
	}
	if seller["displayName"] != nil {
		t.Fatalf("displayName = %#v", seller["displayName"])
	}
}

func TestPublicGetOmitsSellerWhenResolverUnavailable(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	h.profiles = &fakeProfiles{err: identitycontracts.ErrUnavailable}

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertSellerOmitted(t, rec)
	if rec.Header().Get("Cache-Control") != publicListingCacheControl {
		t.Fatalf("cache = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestPublicGetOmitsSellerWhenResolverNotFound(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	h.profiles = &fakeProfiles{err: identitycontracts.ErrNotFound}

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertSellerOmitted(t, rec)
}

func TestPublicGetOmitsSellerWhenResolverNil(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	h.profiles = nil

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertSellerOmitted(t, rec)
}

func TestPublicGetOmitsSellerWhenPublicIDEqualsOwner(t *testing.T) {
	h := newTestHandler(t)
	published := publishListing(t, h, createDraft(t, h))
	h.profiles = &fakeProfiles{profile: identitycontracts.PublicProfile{
		PublicProfileID: identitycontracts.ID(published.OwnerUserID),
	}}

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+published.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertSellerOmitted(t, rec)
	if strings.Contains(rec.Body.String(), published.OwnerUserID.String()) {
		t.Fatalf("owner user id leaked: %s", rec.Body.String())
	}
}

func TestPublicGetDoesNotCallResolverForNonPublished(t *testing.T) {
	h := newTestHandler(t)
	draft := createDraft(t, h)
	profiles := &fakeProfiles{profile: identitycontracts.PublicProfile{PublicProfileID: identitycontracts.ID(mustID(t))}}
	h.profiles = profiles

	rec := do(t, h, http.MethodGet, "/v1/public/listings/"+draft.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if profiles.calls != 0 {
		t.Fatalf("resolver calls = %d", profiles.calls)
	}
}

func assertSellerOmitted(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var raw map[string]any
	decode(t, rec, &raw)
	if _, ok := raw["seller"]; ok {
		t.Fatalf("seller = %#v", raw["seller"])
	}
	if raw["listingId"] == nil || raw["title"] == nil {
		t.Fatalf("listing not readable: %#v", raw)
	}
}

func seedReadyAttachedMedia(t *testing.T, h *testHandler, listingID listings.ID, order int) media.Asset {
	t.Helper()
	owner := media.ID(h.sessions.userID)
	pending, _, err := h.mediaSvc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.mediaObjects.PutUntrusted(pending.ObjectKey, media.ObjectStat{SizeBytes: 10, ContentType: "application/octet-stream"})
	uploaded, err := h.mediaSvc.MarkUploaded(context.Background(), pending.ID, owner, pending.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	processing, err := h.mediaSvc.MarkProcessing(context.Background(), uploaded.ID, owner, uploaded.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := h.mediaSvc.MarkReady(context.Background(), processing.ID, owner, processing.UpdatedAt, media.ValidatedMetadata{
		ContentType: "image/jpeg",
		SizeBytes:   12,
		Width:       10,
		Height:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	attached, err := h.mediaSvc.AttachListing(context.Background(), ready.ID, media.ListingBind{
		ListingID:   media.ID(listingID),
		ActorUserID: owner,
	}, ready.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := attached.WithSortOrder(order, attached.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.mediaStore.Update(context.Background(), ordered, attached.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	return ordered
}

func seedReadyUnattached(t *testing.T, h *testHandler) media.Asset {
	t.Helper()
	owner := media.ID(h.sessions.userID)
	pending, _, err := h.mediaSvc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.mediaObjects.PutUntrusted(pending.ObjectKey, media.ObjectStat{SizeBytes: 10})
	uploaded, err := h.mediaSvc.MarkUploaded(context.Background(), pending.ID, owner, pending.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	processing, err := h.mediaSvc.MarkProcessing(context.Background(), uploaded.ID, owner, uploaded.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := h.mediaSvc.MarkReady(context.Background(), processing.ID, owner, processing.UpdatedAt, media.ValidatedMetadata{
		ContentType: "image/jpeg",
		SizeBytes:   12,
		Width:       10,
		Height:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ready
}

func seedNonReadyAttached(t *testing.T, h *testHandler, listingID listings.ID, status media.Status) media.Asset {
	t.Helper()
	owner := media.ID(h.sessions.userID)
	pending, _, err := h.mediaSvc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	attached, err := h.mediaSvc.AttachListing(context.Background(), pending.ID, media.ListingBind{
		ListingID:   media.ID(listingID),
		ActorUserID: owner,
	}, pending.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	switch status {
	case media.StatusPendingUpload:
		return attached
	case media.StatusUploaded:
		h.mediaObjects.PutUntrusted(attached.ObjectKey, media.ObjectStat{SizeBytes: 4})
		uploaded, err := h.mediaSvc.MarkUploaded(context.Background(), attached.ID, owner, attached.UpdatedAt)
		if err != nil {
			t.Fatal(err)
		}
		return uploaded
	case media.StatusProcessing:
		h.mediaObjects.PutUntrusted(attached.ObjectKey, media.ObjectStat{SizeBytes: 4})
		uploaded, err := h.mediaSvc.MarkUploaded(context.Background(), attached.ID, owner, attached.UpdatedAt)
		if err != nil {
			t.Fatal(err)
		}
		processing, err := h.mediaSvc.MarkProcessing(context.Background(), uploaded.ID, owner, uploaded.UpdatedAt)
		if err != nil {
			t.Fatal(err)
		}
		return processing
	case media.StatusRejected:
		rejected, err := h.mediaSvc.Reject(context.Background(), attached.ID, owner, attached.UpdatedAt)
		if err != nil {
			t.Fatal(err)
		}
		return rejected
	default:
		t.Fatalf("unsupported status %s", status)
		return media.Asset{}
	}
}

func seedDeletedAttached(t *testing.T, h *testHandler, listingID listings.ID) {
	t.Helper()
	owner := media.ID(h.sessions.userID)
	pending, _, err := h.mediaSvc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	attached, err := h.mediaSvc.AttachListing(context.Background(), pending.ID, media.ListingBind{
		ListingID:   media.ID(listingID),
		ActorUserID: owner,
	}, pending.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.mediaSvc.Delete(context.Background(), attached.ID, owner, attached.UpdatedAt); err != nil {
		t.Fatal(err)
	}
}

func publishListing(t *testing.T, h *testHandler, listing listings.Listing) listings.Listing {
	t.Helper()
	if listing.Status == listings.StatusDraft {
		listing = markReady(t, h, listing)
	}
	published, err := h.svc.Publish(context.Background(), listing.ID, listing.UpdatedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	return published
}

func archiveListing(t *testing.T, h *testHandler, listing listings.Listing) listings.Listing {
	t.Helper()
	archived, err := h.svc.Archive(context.Background(), listing.ID, listing.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return archived
}

func restrictListing(t *testing.T, h *testHandler, listing listings.Listing, state listings.ModerationState) listings.Listing {
	t.Helper()
	if err := h.svc.ApplyModerationState(context.Background(), listingcontracts.ApplyModerationInput{
		ListingID: listingcontracts.ID(listing.ID), State: string(state),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := h.svc.Get(context.Background(), listing.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func seedStatus(t *testing.T, h *testHandler, status listings.Status) listings.Listing {
	t.Helper()
	listing := createDraft(t, h)
	expected := listing.UpdatedAt
	listing.Status = status
	listing.UpdatedAt = expected.Add(time.Second)
	if err := h.listingStore.Update(context.Background(), listing, expected); err != nil {
		t.Fatal(err)
	}
	return listing
}
