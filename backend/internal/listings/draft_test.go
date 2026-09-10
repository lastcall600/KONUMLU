package listings

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/location"
	locationcontracts "backend/internal/location/contracts"
	"backend/internal/media"
	mediacontracts "backend/internal/media/contracts"
)

func TestCreateListingDraftWithLocation(t *testing.T) {
	orch, listingStore, locStore, _, _ := mustOrchestrator(t)
	owner := mustID(t)
	got, err := orch.CreateListingDraft(context.Background(), owner, CreateListingDraftInput{
		Content: validContent(mustID(t)),
		Location: &DraftLocation{
			Latitude:  36.621,
			Longitude: 29.116,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerUserID != owner {
		t.Fatalf("owner = %s", got.OwnerUserID)
	}
	if _, err := listingStore.Get(context.Background(), got.ID); err != nil {
		t.Fatal(err)
	}
	loc, err := locStore.GetByListingID(context.Background(), location.ID(got.ID))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Latitude != 36.621 || loc.Longitude != 29.116 {
		t.Fatalf("loc = %+v", loc)
	}
}

func TestCreateListingDraftWithOwnedMedia(t *testing.T) {
	orch, _, _, mediaStore, mediaSvc := mustOrchestrator(t)
	owner := mustID(t)
	asset, _, err := mediaSvc.CreatePending(context.Background(), media.ID(owner), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := orch.CreateListingDraft(context.Background(), owner, CreateListingDraftInput{
		Content:       validContent(mustID(t)),
		MediaAssetIDs: []ID{ID(asset.ID)},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := mediaStore.Get(context.Background(), asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ListingID == nil || media.ID(*stored.ListingID) != media.ID(got.ID) {
		t.Fatalf("attached listing = %v want %s", stored.ListingID, got.ID)
	}
	if stored.OwnerUserID != media.ID(owner) {
		t.Fatalf("media owner = %s", stored.OwnerUserID)
	}
}

func TestCreateListingDraftRejectsForeignMedia(t *testing.T) {
	orch, listingStore, _, _, mediaSvc := mustOrchestrator(t)
	owner := mustID(t)
	other := mustID(t)
	asset, _, err := mediaSvc.CreatePending(context.Background(), media.ID(other), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = orch.CreateListingDraft(context.Background(), owner, CreateListingDraftInput{
		Content:       validContent(mustID(t)),
		MediaAssetIDs: []ID{ID(asset.ID)},
	})
	if !errors.Is(err, errForbidden) {
		t.Fatalf("err = %v", err)
	}
	listed, err := listingStore.ListByOwner(context.Background(), owner)
	if err != nil || len(listed) != 0 {
		t.Fatalf("listing must not be created: %+v err=%v", listed, err)
	}
}

func TestCreateListingDraftRejectsInvalidMediaState(t *testing.T) {
	orch, listingStore, _, _, mediaSvc := mustOrchestrator(t)
	owner := mustID(t)
	asset, _, err := mediaSvc.CreatePending(context.Background(), media.ID(owner), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mediaSvc.Reject(context.Background(), asset.ID, media.ID(owner), asset.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	_, err = orch.CreateListingDraft(context.Background(), owner, CreateListingDraftInput{
		Content:       validContent(mustID(t)),
		MediaAssetIDs: []ID{ID(asset.ID)},
	})
	if !errors.Is(err, mediacontracts.ErrNotUsable) {
		t.Fatalf("err = %v", err)
	}
	listed, err := listingStore.ListByOwner(context.Background(), owner)
	if err != nil || len(listed) != 0 {
		t.Fatalf("listing must not be created: %+v err=%v", listed, err)
	}
}

func TestReplaceOwnedLocationRejectsForeignListing(t *testing.T) {
	geo := &recordingGeo{}
	svc, _, _ := mustService(t)
	orch, err := NewDraftOrchestrator(svc, geo, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := mustID(t)
	other := mustID(t)
	created, err := svc.CreateDraft(context.Background(), owner, validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = orch.ReplaceOwnedLocation(context.Background(), other, created.ID, DraftLocation{Latitude: 36.6, Longitude: 29.1})
	if !errors.Is(err, errForbidden) {
		t.Fatalf("err = %v", err)
	}
	if geo.sets != 0 {
		t.Fatalf("location must not be called: sets=%d", geo.sets)
	}
}

func TestCreateListingDraftUsesSessionOwnerNotClientID(t *testing.T) {
	orch, _, _, _, _ := mustOrchestrator(t)
	session := mustID(t)
	got, err := orch.CreateListingDraft(context.Background(), session, CreateListingDraftInput{
		Content: validContent(mustID(t)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerUserID != session {
		t.Fatalf("owner = %s want session %s", got.OwnerUserID, session)
	}
}

func TestPartialFailureRollsBackSharedTransaction(t *testing.T) {
	listingStore := NewMemoryStore()
	locStore := location.NewMemoryStore()
	mediaStore := media.NewMemoryStore()
	clock := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	listingSvc, err := NewService(listingStore, defaultPublishedForms(), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	locSvc, err := location.NewService(locStore, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	mediaSvc, err := media.NewService(mediaStore, media.NewMemoryObjectStorage(), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	tx := &snapshotTransactor{
		listings: listingStore,
		location: locStore,
		media:    mediaStore,
	}
	orch, err := NewDraftOrchestrator(listingSvc, location.NewGeo(locSvc), media.NewListingMedia(mediaSvc), tx)
	if err != nil {
		t.Fatal(err)
	}
	locStore.SetFail(location.ErrUnavailable)
	owner := mustID(t)
	_, err = orch.CreateListingDraft(context.Background(), owner, CreateListingDraftInput{
		Content:  validContent(mustID(t)),
		Location: &DraftLocation{Latitude: 36.621, Longitude: 29.116},
	})
	if !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
	locStore.SetFail(nil)
	listed, err := listingStore.ListByOwner(context.Background(), owner)
	if err != nil || len(listed) != 0 {
		t.Fatalf("listing rolled back: %+v err=%v", listed, err)
	}
	if len(locStore.Snapshot()) != 0 {
		t.Fatalf("location rolled back: %+v", locStore.Snapshot())
	}
}

type recordingGeo struct {
	sets int
}

func (r *recordingGeo) SetListingLocation(context.Context, locationcontracts.ListingWriteScope, locationcontracts.Coordinates, *locationcontracts.ID) (locationcontracts.ListingPoint, error) {
	r.sets++
	return locationcontracts.ListingPoint{}, nil
}

func mustOrchestrator(t *testing.T) (*DraftOrchestrator, *MemoryStore, *location.MemoryStore, *media.MemoryStore, *media.Service) {
	t.Helper()
	listingStore := NewMemoryStore()
	locStore := location.NewMemoryStore()
	mediaStore := media.NewMemoryStore()
	clock := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	listingSvc, err := NewService(listingStore, defaultPublishedForms(), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	locSvc, err := location.NewService(locStore, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	mediaSvc, err := media.NewService(mediaStore, media.NewMemoryObjectStorage(), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	tx := &snapshotTransactor{
		listings: listingStore,
		location: locStore,
		media:    mediaStore,
	}
	orch, err := NewDraftOrchestrator(listingSvc, location.NewGeo(locSvc), media.NewListingMedia(mediaSvc), tx)
	if err != nil {
		t.Fatal(err)
	}
	return orch, listingStore, locStore, mediaStore, mediaSvc
}

type snapshotTransactor struct {
	listings *MemoryStore
	location *location.MemoryStore
	media    *media.MemoryStore
}

func (s *snapshotTransactor) Begin(context.Context) (Tx, error) {
	return &snapshotTx{
		listings:     s.listings,
		location:     s.location,
		media:        s.media,
		listingSnap:  s.listings.Snapshot(),
		locationSnap: s.location.Snapshot(),
		mediaSnap:    s.media.Snapshot(),
	}, nil
}

type snapshotTx struct {
	listings     *MemoryStore
	location     *location.MemoryStore
	media        *media.MemoryStore
	listingSnap  map[ID]Listing
	locationSnap map[location.ID]location.ListingLocation
	mediaSnap    map[media.ID]media.Asset
	committed    bool
}

func (t *snapshotTx) Context(ctx context.Context) context.Context { return ctx }

func (t *snapshotTx) Commit(context.Context) error {
	t.committed = true
	return nil
}

func (t *snapshotTx) Rollback(context.Context) error {
	if t.committed {
		return nil
	}
	t.listings.Restore(t.listingSnap)
	t.location.Restore(t.locationSnap)
	t.media.Restore(t.mediaSnap)
	return nil
}
