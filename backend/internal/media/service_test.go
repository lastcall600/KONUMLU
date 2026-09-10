package media

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/platform/db"
)

type frozenNow struct {
	now time.Time
}

func mustService(t *testing.T) (*Service, *MemoryStore, *MemoryObjectStorage, *frozenNow) {
	t.Helper()
	store := NewMemoryStore()
	objects := NewMemoryObjectStorage()
	clock := &frozenNow{now: time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)}
	svc, err := NewService(store, objects, func() time.Time { return clock.now })
	if err != nil {
		t.Fatal(err)
	}
	svc.SetOutbox(store, &MemoryEnqueuer{})
	return svc, store, objects, clock
}

func TestServiceCreatePendingServerKey(t *testing.T) {
	svc, store, _, _ := mustService(t)
	owner := mustID(t)
	filename := "../secret.jpg"
	asset, target, err := svc.CreatePending(context.Background(), owner, &filename)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Status != StatusPendingUpload {
		t.Fatalf("status = %s", asset.Status)
	}
	if target.ObjectKey != asset.ObjectKey || !strings.HasPrefix(target.ObjectKey, ObjectKeyPrefix(owner, asset.ID)) {
		t.Fatalf("target key = %q asset = %q", target.ObjectKey, asset.ObjectKey)
	}
	if asset.OriginalFilename == nil || *asset.OriginalFilename != "secret.jpg" {
		t.Fatalf("filename = %v", asset.OriginalFilename)
	}
	stored, err := store.Get(context.Background(), asset.ID)
	if err != nil || stored.ObjectKey != asset.ObjectKey {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
}

func TestServiceConfirmUploadMissingOversizedAndValid(t *testing.T) {
	svc, _, objects, clock := mustService(t)
	svc.SetMaxUploadBytes(100)
	owner := mustID(t)
	asset, _, err := svc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Minute)
	if _, err := svc.ConfirmUpload(context.Background(), asset.ID, owner); !errors.Is(err, errObjectMissing) {
		t.Fatalf("missing object err = %v", err)
	}
	got, err := svc.Get(context.Background(), asset.ID, owner)
	if err != nil || got.Status != StatusPendingUpload {
		t.Fatalf("missing object must stay pending: %+v err=%v", got, err)
	}

	objects.PutUntrusted(asset.ObjectKey, ObjectStat{SizeBytes: 101, ContentType: "image/png"})
	clock.now = clock.now.Add(time.Second)
	rejected, err := svc.ConfirmUpload(context.Background(), asset.ID, owner)
	if !errors.Is(err, errObjectTooLarge) {
		t.Fatalf("oversized err = %v", err)
	}
	if rejected.Status != StatusRejected || rejected.ContentType != nil {
		t.Fatalf("oversized must reject untrusted: %+v", rejected)
	}
	if len(objects.DeletedKeys()) != 1 || objects.DeletedKeys()[0] != asset.ObjectKey {
		t.Fatalf("delete attempted = %v", objects.DeletedKeys())
	}

	okAsset, _, err := svc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	objects.PutUntrusted(okAsset.ObjectKey, ObjectStat{SizeBytes: 50, ContentType: "application/octet-stream"})
	clock.now = clock.now.Add(time.Second)
	uploaded, err := svc.ConfirmUpload(context.Background(), okAsset.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.Status != StatusUploaded || uploaded.ReadyAt != nil || uploaded.ContentType != nil || uploaded.SizeBytes != nil {
		t.Fatalf("confirm is uploaded/untrusted only: %+v", uploaded)
	}
}

func TestServiceMarkUploadedRequiresObject(t *testing.T) {
	svc, _, objects, clock := mustService(t)
	owner := mustID(t)
	asset, _, err := svc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Minute)
	if _, err := svc.MarkUploaded(context.Background(), asset.ID, owner, asset.UpdatedAt); !errors.Is(err, errObjectMissing) {
		t.Fatalf("missing object err = %v", err)
	}
	objects.PutUntrusted(asset.ObjectKey, ObjectStat{SizeBytes: 99, ContentType: "application/octet-stream"})
	uploaded, err := svc.MarkUploaded(context.Background(), asset.ID, owner, asset.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.Status != StatusUploaded || uploaded.ContentType != nil {
		t.Fatalf("upload remains untrusted: %+v", uploaded)
	}
}

func TestServiceReadyAndListingMedia(t *testing.T) {
	svc, _, objects, clock := mustService(t)
	owner := mustID(t)
	listing := mustID(t)
	a := createReadyAttached(t, svc, objects, clock, owner, listing)
	media, err := svc.ListListingMedia(context.Background(), listing)
	if err != nil || len(media) != 1 || media[0].ID != a.ID {
		t.Fatalf("media = %+v err = %v", media, err)
	}
}

func TestServiceRejectedNotListed(t *testing.T) {
	svc, _, objects, clock := mustService(t)
	owner := mustID(t)
	listing := mustID(t)
	pending, _, err := svc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	attached, err := svc.AttachListing(context.Background(), pending.ID, ListingBind{ListingID: listing, ActorUserID: owner}, pending.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	objects.PutUntrusted(attached.ObjectKey, ObjectStat{SizeBytes: 1})
	clock.now = clock.now.Add(time.Second)
	uploaded, err := svc.MarkUploaded(context.Background(), attached.ID, owner, attached.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	rejected, err := svc.Reject(context.Background(), uploaded.ID, owner, uploaded.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if rejected.UsableAsListingMedia() {
		t.Fatal("rejected listed")
	}
	media, err := svc.ListListingMedia(context.Background(), listing)
	if err != nil || len(media) != 0 {
		t.Fatalf("media = %+v err = %v", media, err)
	}
}

func TestServiceOwnerChecks(t *testing.T) {
	svc, _, _, clock := mustService(t)
	owner := mustID(t)
	other := mustID(t)
	asset, _, err := svc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), asset.ID, other); !errors.Is(err, errForbidden) {
		t.Fatalf("get err = %v", err)
	}
	clock.now = clock.now.Add(time.Second)
	if _, err := svc.AttachListing(context.Background(), asset.ID, ListingBind{ListingID: mustID(t), ActorUserID: other}, asset.UpdatedAt); !errors.Is(err, errForbidden) {
		t.Fatalf("attach err = %v", err)
	}
}

func TestServiceAttachRequiresOwnershipContextAndUsableState(t *testing.T) {
	svc, _, _, clock := mustService(t)
	owner := mustID(t)
	asset, _, err := svc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachListing(context.Background(), asset.ID, ListingBind{ActorUserID: owner}, asset.UpdatedAt); !errors.Is(err, errZeroID) {
		t.Fatalf("missing listing bind err = %v", err)
	}
	clock.now = clock.now.Add(time.Second)
	rejected, err := svc.Reject(context.Background(), asset.ID, owner, asset.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AssertAttachable(context.Background(), rejected.ID, owner); !errors.Is(err, errNotUsable) {
		t.Fatalf("rejected attachable err = %v", err)
	}
	if _, err := svc.AttachListing(context.Background(), rejected.ID, ListingBind{ListingID: mustID(t), ActorUserID: owner}, rejected.UpdatedAt); !errors.Is(err, errNotUsable) {
		t.Fatalf("rejected attach err = %v", err)
	}

	ok, _, err := svc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	listing := mustID(t)
	attached, err := svc.AttachListing(context.Background(), ok.ID, ListingBind{ListingID: listing, ActorUserID: owner}, ok.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AssertAttachable(context.Background(), attached.ID, owner); !errors.Is(err, errAlreadyAttached) {
		t.Fatalf("already attached err = %v", err)
	}
	if _, err := svc.AttachListing(context.Background(), attached.ID, ListingBind{ListingID: mustID(t), ActorUserID: owner}, attached.UpdatedAt); !errors.Is(err, errAlreadyAttached) {
		t.Fatalf("rebind err = %v", err)
	}
}

func TestServiceOrdering(t *testing.T) {
	svc, _, objects, clock := mustService(t)
	owner := mustID(t)
	listing := mustID(t)
	first := createReadyAttached(t, svc, objects, clock, owner, listing)
	second := createReadyAttached(t, svc, objects, clock, owner, listing)
	ordered, err := svc.OrderListingImages(context.Background(), owner, listing, []ID{second.ID, first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 2 || *ordered[0].SortOrder != 0 || ordered[0].ID != second.ID {
		t.Fatalf("ordered = %+v", ordered)
	}
	listed, err := svc.ListListingMedia(context.Background(), listing)
	if err != nil || len(listed) != 2 || listed[0].ID != second.ID || listed[1].ID != first.ID {
		t.Fatalf("listed = %+v err = %v", listed, err)
	}
	if _, err := svc.OrderListingImages(context.Background(), owner, listing, []ID{first.ID}); !errors.Is(err, errInvalidOrder) {
		t.Fatalf("partial order err = %v", err)
	}
	if _, err := svc.OrderListingImages(context.Background(), otherOwner(t, owner), listing, []ID{second.ID, first.ID}); !errors.Is(err, errForbidden) {
		t.Fatalf("other owner err = %v", err)
	}
}

func TestServiceListPublicListingMediaReadyOnly(t *testing.T) {
	svc, store, objects, clock := mustService(t)
	owner := mustID(t)
	listing := mustID(t)
	other := mustID(t)
	ready := createReadyAttached(t, svc, objects, clock, owner, listing)
	clock.now = clock.now.Add(time.Second)
	ordered, err := ready.WithSortOrder(2, clock.now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), ordered, ready.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	_ = createReadyAttached(t, svc, objects, clock, owner, other)
	pending, _, err := svc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	if _, err := svc.AttachListing(context.Background(), pending.ID, ListingBind{ListingID: listing, ActorUserID: owner}, pending.UpdatedAt); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ListPublicListingMedia(context.Background(), listing)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AssetID != ordered.ID || got[0].Order != 2 {
		t.Fatalf("got = %+v", got)
	}
	if got[0].URL == "" || got[0].Kind != KindListingImage {
		t.Fatalf("item = %+v", got[0])
	}
	if got[0].Width == nil || got[0].Height == nil {
		t.Fatal("expected dimensions")
	}
	if strings.Contains(got[0].URL, ordered.ObjectKey) {
		t.Fatal("original object key in public URL")
	}
}

func TestServiceListPublicListingMediaStoreUnavailable(t *testing.T) {
	svc, store, _, _ := mustService(t)
	store.SetFail(db.ErrUnavailable)
	if _, err := svc.ListPublicListingMedia(context.Background(), mustID(t)); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestServicePersistenceErrorMapping(t *testing.T) {
	store := NewMemoryStore()
	objects := NewMemoryObjectStorage()
	svc, err := NewService(store, objects, func() time.Time { return time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	store.SetFail(db.ErrUnavailable)
	if _, _, err := svc.CreatePending(context.Background(), mustID(t), nil); !errors.Is(err, errUnavailable) {
		t.Fatalf("unavailable err = %v", err)
	}
	store.SetFail(db.ErrNoRows)
	if _, err := svc.Get(context.Background(), mustID(t), mustID(t)); !errors.Is(err, errNotFound) {
		t.Fatalf("not found err = %v", err)
	}
	store.SetFail(errors.New("driver boom"))
	if _, err := svc.ListListingMedia(context.Background(), mustID(t)); !errors.Is(err, errUnavailable) {
		t.Fatalf("unknown err = %v", err)
	}
	if _, err := NewService(nil, objects, nil); !errors.Is(err, errStoreRequired) {
		t.Fatalf("nil store err = %v", err)
	}
	if _, err := NewService(store, nil, nil); !errors.Is(err, errStorageRequired) {
		t.Fatalf("nil storage err = %v", err)
	}
}

func createReadyAttached(t *testing.T, svc *Service, objects *MemoryObjectStorage, clock *frozenNow, owner, listing ID) Asset {
	t.Helper()
	pending, _, err := svc.CreatePending(context.Background(), owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	objects.PutUntrusted(pending.ObjectKey, ObjectStat{SizeBytes: 10, ContentType: "application/octet-stream"})
	clock.now = clock.now.Add(time.Second)
	uploaded, err := svc.MarkUploaded(context.Background(), pending.ID, owner, pending.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	processing, err := svc.MarkProcessing(context.Background(), uploaded.ID, owner, uploaded.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	ready, err := svc.MarkReady(context.Background(), processing.ID, owner, processing.UpdatedAt, validMeta())
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	attached, err := svc.AttachListing(context.Background(), ready.ID, ListingBind{ListingID: listing, ActorUserID: owner}, ready.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return attached
}

func otherOwner(t *testing.T, owner ID) ID {
	t.Helper()
	for {
		id := mustID(t)
		if id != owner {
			return id
		}
	}
}
