package media

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewPendingAssetGeneratesServerObjectKey(t *testing.T) {
	now := time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)
	owner := mustID(t)
	filename := "photo.jpg"
	got, err := NewPendingAsset(owner, &filename, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPendingUpload || got.Kind != KindListingImage {
		t.Fatalf("asset = %+v", got)
	}
	if !strings.HasPrefix(got.ObjectKey, ObjectKeyPrefix(owner, got.ID)) {
		t.Fatalf("object key = %q", got.ObjectKey)
	}
	if !IsServerObjectKey(got.ObjectKey) {
		t.Fatalf("object key rejected: %q", got.ObjectKey)
	}
	if strings.Contains(got.ObjectKey, "photo") || strings.Contains(got.ObjectKey, ".jpg") {
		t.Fatalf("client filename leaked into object key: %q", got.ObjectKey)
	}
	if strings.Contains(got.ObjectKey, "http") || strings.Contains(got.ObjectKey, "s3") {
		t.Fatalf("object key must not be a URL: %q", got.ObjectKey)
	}
	other, err := NewPendingAsset(owner, &filename, now)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID != got.ID && other.ObjectKey == got.ObjectKey {
		t.Fatal("object keys must not collide across assets")
	}
	if got.ListingID != nil || got.ContentType != nil || got.SizeBytes != nil {
		t.Fatalf("pending must not store trusted metadata: %+v", got)
	}
	if got.OriginalFilename == nil || *got.OriginalFilename != "photo.jpg" {
		t.Fatalf("filename = %v", got.OriginalFilename)
	}
	if got.UsableAsListingMedia() {
		t.Fatal("pending asset must not be usable listing media")
	}
}

func TestObjectKeyRejectsClientChosenValue(t *testing.T) {
	a := mustPending(t)
	a.ObjectKey = "uploads/evil.jpg"
	if err := a.Validate(); !errors.Is(err, errInvalidObjectKey) {
		t.Fatalf("err = %v", err)
	}
	a = mustPending(t)
	a.ObjectKey = ObjectKeyPrefix(a.OwnerUserID, a.ID) + "../secret"
	if err := a.Validate(); !errors.Is(err, errInvalidObjectKey) {
		t.Fatalf("traversal err = %v", err)
	}
}

func TestIsServerObjectKeyRejectsUnsafeValues(t *testing.T) {
	owner := mustID(t)
	asset := mustID(t)
	ok := ObjectKeyFor(owner, asset, strings.Repeat("ab", 16))
	if len(ok) < 32 || !IsServerObjectKey(ok) {
		t.Fatalf("want valid key, got %q", ok)
	}
	rejects := []string{
		"",
		"listing-images/" + owner.String() + "/" + asset.String(),
		"media/listing-images/../" + owner.String() + "/" + asset.String() + "/" + strings.Repeat("ab", 16),
		"media/listing-images/" + owner.String() + "/" + asset.String() + "/photo.jpg",
		"media/listing-images/" + owner.String() + "/" + asset.String() + "/" + strings.Repeat("ab", 16) + "/extra",
		"media/listing-images/" + owner.String() + "/" + asset.String() + "/p/" + strings.Repeat("ab", 16) + "/extra",
		"/media/listing-images/" + owner.String() + "/" + asset.String() + "/" + strings.Repeat("ab", 16),
	}
	processed := ProcessedObjectKeyFor(owner, asset, strings.Repeat("ab", 16))
	if !IsProcessedObjectKey(processed) || !IsServerObjectKey(processed) || IsOriginalObjectKey(processed) {
		t.Fatalf("processed key rejected: %q", processed)
	}
	if processed == ok {
		t.Fatal("processed key must differ from original")
	}
	for _, key := range rejects {
		if IsServerObjectKey(key) {
			t.Fatalf("accepted unsafe key %q", key)
		}
	}
}

func TestInvalidLifecycleTransitions(t *testing.T) {
	a := mustPending(t)
	at := a.CreatedAt.Add(time.Minute)
	if _, err := a.MarkProcessing(at); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("processing from pending err = %v", err)
	}
	if _, err := a.MarkReady(validMeta(), mustProcessedKey(t, a), at); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("ready from pending err = %v", err)
	}
	uploaded, err := a.MarkUploaded(at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uploaded.MarkReady(validMeta(), mustProcessedKey(t, uploaded), at.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("ready from uploaded err = %v", err)
	}
	if _, err := uploaded.MarkUploaded(at.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("double uploaded err = %v", err)
	}
}

func TestReadyRequiresValidatedMetadata(t *testing.T) {
	a := mustProcessing(t)
	at := a.UpdatedAt.Add(time.Second)
	processed := mustProcessedKey(t, a)
	if _, err := a.MarkReady(ValidatedMetadata{}, processed, at); !errors.Is(err, errInvalidMetadata) {
		t.Fatalf("empty meta err = %v", err)
	}
	if _, err := a.MarkReady(ValidatedMetadata{ContentType: "image/jpeg", SizeBytes: 0, Width: 10, Height: 10}, processed, at); !errors.Is(err, errInvalidMetadata) {
		t.Fatalf("size err = %v", err)
	}
	if _, err := a.MarkReady(ValidatedMetadata{ContentType: "application/octet-stream", SizeBytes: 10, Width: 10, Height: 10}, processed, at); !errors.Is(err, errInvalidMetadata) {
		t.Fatalf("type err = %v", err)
	}
	if _, err := a.MarkReady(validMeta(), a.ObjectKey, at); !errors.Is(err, errInvalidObjectKey) {
		t.Fatalf("original key as processed err = %v", err)
	}
	got, err := a.MarkReady(validMeta(), processed, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusReady || got.ReadyAt == nil || got.ContentType == nil || *got.ContentType != "image/jpeg" {
		t.Fatalf("ready = %+v", got)
	}
	if got.ProcessedObjectKey == got.ObjectKey || !IsProcessedObjectKey(got.ProcessedObjectKey) {
		t.Fatalf("processed key = %q original = %q", got.ProcessedObjectKey, got.ObjectKey)
	}
}

func TestRejectedAndDeletedNotUsable(t *testing.T) {
	pending := mustPending(t)
	listing := mustID(t)
	at := pending.CreatedAt.Add(time.Minute)
	rejected, err := pending.Reject(at)
	if err != nil {
		t.Fatal(err)
	}
	if rejected.UsableAsListingMedia() {
		t.Fatal("rejected must not be usable")
	}
	if _, err := rejected.AttachListing(listing, at.Add(time.Second)); !errors.Is(err, errNotUsable) {
		t.Fatalf("attach rejected err = %v", err)
	}
	ready := mustReady(t)
	deleted, err := ready.Delete(ready.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if deleted.UsableAsListingMedia() {
		t.Fatal("deleted must not be usable")
	}
	if _, err := deleted.DetachListing(deleted.UpdatedAt.Add(time.Second)); !errors.Is(err, errNotUsable) {
		t.Fatalf("detach deleted err = %v", err)
	}
	if _, err := deleted.Delete(deleted.UpdatedAt.Add(time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("double delete err = %v", err)
	}
}

func TestAttachDetachListing(t *testing.T) {
	a := mustPending(t)
	listing := mustID(t)
	at := a.CreatedAt.Add(time.Minute)
	attached, err := a.AttachListing(listing, at)
	if err != nil {
		t.Fatal(err)
	}
	if attached.ListingID == nil || *attached.ListingID != listing {
		t.Fatalf("listing = %v", attached.ListingID)
	}
	if attached.UsableAsListingMedia() {
		t.Fatal("attached pending is not listing media")
	}
	if _, err := attached.AttachListing(mustID(t), at.Add(time.Second)); !errors.Is(err, errAlreadyAttached) {
		t.Fatalf("double attach err = %v", err)
	}
	detached, err := attached.DetachListing(at.Add(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if detached.ListingID != nil || detached.SortOrder != nil {
		t.Fatalf("detached = %+v", detached)
	}
}

func TestReadyListingMediaRequiresListing(t *testing.T) {
	ready := mustReady(t)
	if ready.UsableAsListingMedia() {
		t.Fatal("ready without listing must not be exposed")
	}
	got, err := ready.AttachListing(mustID(t), ready.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !got.UsableAsListingMedia() {
		t.Fatal("ready attached asset must be usable listing media")
	}
}

func TestFilenameIsHintNotPath(t *testing.T) {
	now := time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)
	path := `C:\temp\nested\pic.png`
	got, err := NewPendingAsset(mustID(t), &path, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.OriginalFilename == nil || *got.OriginalFilename != "pic.png" {
		t.Fatalf("filename = %v", got.OriginalFilename)
	}
}

func validMeta() ValidatedMetadata {
	return ValidatedMetadata{ContentType: "image/jpeg", SizeBytes: 1200, Width: 800, Height: 600}
}

func mustPending(t *testing.T) Asset {
	t.Helper()
	now := time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)
	a, err := NewPendingAsset(mustID(t), nil, now)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func mustProcessing(t *testing.T) Asset {
	t.Helper()
	a := mustPending(t)
	uploaded, err := a.MarkUploaded(a.CreatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	processing, err := uploaded.MarkProcessing(uploaded.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return processing
}

func mustReady(t *testing.T) Asset {
	t.Helper()
	a := mustProcessing(t)
	ready, err := a.MarkReady(validMeta(), mustProcessedKey(t, a), a.UpdatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return ready
}

func mustProcessedKey(t *testing.T, a Asset) string {
	t.Helper()
	key, err := ProcessedObjectKeyFromOriginal(a.OwnerUserID, a.ID, a.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
