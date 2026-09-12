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

	"backend/internal/listings/contracts"
	"backend/internal/media"
	"backend/internal/platform/db"
)

const allowedOrigin = "https://app.example.test"

func TestCreateRequiresAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)

	rec := do(t, h.Handler, http.MethodPost, "/v1/media/listing-images", allowedOrigin, nil, map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")

	rec = do(t, h, http.MethodPost, "/v1/media/listing-images", "", nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")

	rec = do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, nil, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")
}

func TestCreateOwnerDerivedFromSessionNotClient(t *testing.T) {
	h := newTestHandler(t)
	clientOwner := mustID(t)
	clientKey := "media/listing-images/" + clientOwner.String() + "/" + clientOwner.String() + "/" + strings.Repeat("ab", 16)

	rec := do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, map[string]any{
		"ownerUserId":      clientOwner.String(),
		"owner_user_id":    clientOwner.String(),
		"objectKey":        clientKey,
		"object_key":       "../etc/passwd",
		"originalFilename": "photo.jpg",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body createResponse
	decode(t, rec, &body)
	if body.AssetID == "" || body.UploadURL == "" || body.ExpiresAt == "" || body.MaxBytes <= 0 {
		t.Fatalf("signed target missing: %+v", body)
	}
	if strings.Contains(rec.Body.String(), "objectKey") || strings.Contains(rec.Body.String(), "object_key") {
		t.Fatal("must not return object key")
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "secret") || strings.Contains(rec.Body.String(), "minioadmin") {
		t.Fatal("must not return storage credentials")
	}
	assetID, err := media.ParseID(body.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := h.store.Get(context.Background(), assetID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OwnerUserID != h.sessions.userID {
		t.Fatalf("owner = %s want session %s", stored.OwnerUserID, h.sessions.userID)
	}
	if stored.OwnerUserID == clientOwner {
		t.Fatal("must not use client owner_user_id")
	}
	if stored.ObjectKey == clientKey || strings.Contains(stored.ObjectKey, "..") {
		t.Fatalf("client must not choose object key: %q", stored.ObjectKey)
	}
	if !media.IsServerObjectKey(stored.ObjectKey) {
		t.Fatalf("server key invalid: %q", stored.ObjectKey)
	}
	if stored.OriginalFilename == nil || *stored.OriginalFilename != "photo.jpg" {
		t.Fatalf("filename = %v", stored.OriginalFilename)
	}
}

func TestConfirmMissingObjectFails(t *testing.T) {
	h := newTestHandler(t)
	asset, _ := createPending(t, h)

	rec := do(t, h, http.MethodPost, "/v1/media/listing-images/"+asset.ID.String()+"/confirm", allowedOrigin, map[string]any{
		"objectKey":   "../other",
		"contentType": "image/png",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "bad_request")
	got, err := h.store.Get(context.Background(), asset.ID)
	if err != nil || got.Status != media.StatusPendingUpload {
		t.Fatalf("must stay pending: %+v err=%v", got, err)
	}
}

func TestConfirmOversizedRejectsAndDeletes(t *testing.T) {
	h := newTestHandler(t)
	h.svc.SetMaxUploadBytes(50)
	asset, _ := createPending(t, h)
	h.objects.PutUntrusted(asset.ObjectKey, media.ObjectStat{SizeBytes: 51, ContentType: "image/jpeg"})

	rec := do(t, h, http.MethodPost, "/v1/media/listing-images/"+asset.ID.String()+"/confirm", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := h.store.Get(context.Background(), asset.ID)
	if err != nil || got.Status != media.StatusRejected {
		t.Fatalf("must reject: %+v err=%v", got, err)
	}
	if len(h.objects.DeletedKeys()) != 1 {
		t.Fatalf("delete attempted = %v", h.objects.DeletedKeys())
	}
	assertNoSensitiveLeak(t, rec.Body.String())
}

func TestConfirmValidObjectUploadedOnly(t *testing.T) {
	h := newTestHandler(t)
	asset, _ := createPending(t, h)
	h.objects.PutUntrusted(asset.ObjectKey, media.ObjectStat{SizeBytes: 12, ContentType: "image/png"})

	rec := do(t, h, http.MethodPost, "/v1/media/listing-images/"+asset.ID.String()+"/confirm", allowedOrigin, map[string]any{
		"contentType": "image/png",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body confirmResponse
	decode(t, rec, &body)
	if body.AssetID != asset.ID.String() || body.Status != string(media.StatusUploaded) {
		t.Fatalf("body = %+v", body)
	}
	got, err := h.store.Get(context.Background(), asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != media.StatusUploaded || got.ReadyAt != nil || got.ContentType != nil || got.SizeBytes != nil {
		t.Fatalf("must remain untrusted uploaded: %+v", got)
	}
	if strings.Contains(rec.Body.String(), "ready") {
		t.Fatal("confirm must not mark or advertise ready")
	}
}

func TestNonOwnerDeniedPrivacySafe(t *testing.T) {
	h := newTestHandler(t)
	asset, _ := createPending(t, h)
	h.objects.PutUntrusted(asset.ObjectKey, media.ObjectStat{SizeBytes: 8})
	h.sessions.userID = mustID(t)

	rec := do(t, h, http.MethodPost, "/v1/media/listing-images/"+asset.ID.String()+"/confirm", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("confirm status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
	assertNoSensitiveLeak(t, rec.Body.String())

	rec = do(t, h, http.MethodGet, "/v1/media/listing-images/"+asset.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "not_found")
}

func TestGetReturnsMetadataOnly(t *testing.T) {
	h := newTestHandler(t)
	asset, _ := createPending(t, h)

	rec := do(t, h, http.MethodGet, "/v1/media/listing-images/"+asset.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body metadataResponse
	decode(t, rec, &body)
	if body.AssetID != asset.ID.String() || body.Status != string(media.StatusPendingUpload) {
		t.Fatalf("body = %+v", body)
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "uploadUrl") || strings.Contains(raw, "objectKey") || strings.Contains(raw, "http") {
		t.Fatalf("must not return object URL: %s", raw)
	}
}

func TestCreateWithOwnedListingAttaches(t *testing.T) {
	h := newTestHandler(t)
	listingID := mustID(t)
	h.listings = &fakeOwnership{owner: contracts.ID(h.sessions.userID)}

	rec := do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, map[string]any{
		"listingId": listingID.String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body createResponse
	decode(t, rec, &body)
	assetID, err := media.ParseID(body.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := h.store.Get(context.Background(), assetID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ListingID == nil || *stored.ListingID != listingID {
		t.Fatalf("listing = %v want %s", stored.ListingID, listingID)
	}
}

func TestCreateWithForeignListingIsPrivacySafe404(t *testing.T) {
	h := newTestHandler(t)
	listingID := mustID(t)
	h.listings = &fakeOwnership{err: contracts.ErrForbidden}
	before := len(h.store.Snapshot())

	rec := do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, map[string]any{
		"listingId":   listingID.String(),
		"ownerUserId": h.sessions.userID.String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
	assertNoSensitiveLeak(t, rec.Body.String())
	if len(h.store.Snapshot()) != before {
		t.Fatal("must not create media for a foreign listing")
	}
}

func TestCreateWithMissingListingIsPrivacySafe404(t *testing.T) {
	h := newTestHandler(t)
	listingID := mustID(t)
	h.listings = &fakeOwnership{err: contracts.ErrNotFound}
	before := len(h.store.Snapshot())

	rec := do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, map[string]any{
		"listingId": listingID.String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
	if len(h.store.Snapshot()) != before {
		t.Fatal("must not create media for a missing listing")
	}
}

func TestCreateListingOwnershipInfrastructureIs503(t *testing.T) {
	h := newTestHandler(t)
	h.listings = &fakeOwnership{err: errors.New("connection refused")}
	rec := do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, map[string]any{
		"listingId": mustID(t).String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())
}

func TestStorageDisabledIs503(t *testing.T) {
	h, err := New(&fakeSessions{userID: mustID(t)}, nil, nil, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
}

func TestStorageFailureIs503WithoutProviderDetail(t *testing.T) {
	h := newTestHandler(t)
	h.objects.SetFail(errors.New("AccessDenied MinIO NoSuchBucket s3.amazonaws.com AKIASECRET"))
	rec := do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "unavailable")
	assertNoSensitiveLeak(t, rec.Body.String())
	if strings.Contains(rec.Body.String(), "MinIO") || strings.Contains(rec.Body.String(), "AKIA") ||
		strings.Contains(rec.Body.String(), "s3.amazonaws") || strings.Contains(rec.Body.String(), "NoSuchBucket") {
		t.Fatal("must not leak provider errors")
	}

	h.store.SetFail(db.ErrUnavailable)
	h.objects.SetFail(nil)
	rec = do(t, h, http.MethodPost, "/v1/media/listing-images", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("db status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unavailable")
}

func TestConfirmDuplicateIsIdempotent(t *testing.T) {
	h := newTestHandler(t)
	asset, _ := createPending(t, h)
	h.objects.PutUntrusted(asset.ObjectKey, media.ObjectStat{SizeBytes: 12, ContentType: "image/png"})
	path := "/v1/media/listing-images/" + asset.ID.String() + "/confirm"
	rec := do(t, h, http.MethodPost, path, allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, path, allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body confirmResponse
	decode(t, rec, &body)
	if body.Status != string(media.StatusUploaded) {
		t.Fatalf("body = %+v", body)
	}
}

func TestGetRequiresAuth(t *testing.T) {
	h := newTestHandler(t)
	asset, _ := createPending(t, h)
	rec := do(t, h, http.MethodGet, "/v1/media/listing-images/"+asset.ID.String(), "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestMalformedAssetIDRejected(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/media/listing-images/not-a-uuid", "", nil, authedCookies())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "bad_request")
}

type testHandler struct {
	*Handler
	sessions *fakeSessions
	store    *media.MemoryStore
	objects  *media.MemoryObjectStorage
}

func newTestHandler(t *testing.T) *testHandler {
	t.Helper()
	store := media.NewMemoryStore()
	objects := media.NewMemoryObjectStorage()
	objects.MaxBytes = 100
	svc, err := media.NewService(store, objects, func() time.Time {
		return time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetMaxUploadBytes(100)
	sessions := &fakeSessions{userID: mustID(t)}
	h, err := New(sessions, svc, nil, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return &testHandler{Handler: h, sessions: sessions, store: store, objects: objects}
}

func createPending(t *testing.T, h *testHandler) (media.Asset, media.UploadTarget) {
	t.Helper()
	asset, target, err := h.svc.CreatePending(context.Background(), h.sessions.userID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return asset, target
}

type fakeOwnership struct {
	owner contracts.ID
	err   error
}

func (f *fakeOwnership) ResolveListingOwner(ctx context.Context, listingID contracts.ID) (contracts.ListingRef, error) {
	if err := f.AssertListingOwnedBy(ctx, listingID, f.owner); err != nil {
		return contracts.ListingRef{}, err
	}
	return contracts.ListingRef{ID: listingID, OwnerUserID: f.owner, Status: "draft"}, nil
}

func (f *fakeOwnership) AssertListingOwnedBy(_ context.Context, listingID, userID contracts.ID) error {
	if f.err != nil {
		return f.err
	}
	if listingID.IsZero() || userID.IsZero() {
		return contracts.ErrZeroID
	}
	if !f.owner.IsZero() && f.owner != userID {
		return contracts.ErrForbidden
	}
	return nil
}

type fakeSessions struct {
	userID media.ID
	err    error
}

func (f *fakeSessions) Resolve(context.Context, string) (media.ID, error) {
	if f.err != nil {
		return media.ID{}, f.err
	}
	return f.userID, nil
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
	banned := []string{
		"postgres", "sql:", "minio", "s3", "accessdenied", "stack", "panic",
		"secret", "credential", "akia",
	}
	for _, w := range banned {
		if strings.Contains(lower, w) {
			t.Fatalf("sensitive token %q leaked in %q", w, body)
		}
	}
}

func mustID(t *testing.T) media.ID {
	t.Helper()
	id, err := media.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
