package media

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
)

// Service is media upload and processing orchestration. Storage vendors stay behind ObjectStorage.
type Service struct {
	store          assetStore
	objects        ObjectStorage
	now            func() time.Time
	maxUploadBytes int64
	tx             transactor
	outbox         processEnqueuer
	malware        MalwareScanner
	moderator      ImageModerator
	policy         ProcessingPolicy
}

type transactor interface {
	Begin(ctx context.Context) (transaction, error)
}

type transaction interface {
	outbox.Execer
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type processEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

func NewService(store assetStore, objects ObjectStorage, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if objects == nil {
		return nil, errStorageRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, objects: objects, now: now}, nil
}

// SetMaxUploadBytes sets the trusted Stat size ceiling used by ConfirmUpload.
// Zero disables the size check (tests of other transitions).
func (s *Service) SetMaxUploadBytes(n int64) {
	if s == nil || n < 0 {
		return
	}
	s.maxUploadBytes = n
}

func (s *Service) SetOutbox(tx transactor, enqueuer processEnqueuer) {
	if s == nil {
		return
	}
	s.tx = tx
	s.outbox = enqueuer
}

func (s *Service) SetProcessingPorts(malware MalwareScanner, moderator ImageModerator) {
	if s == nil {
		return
	}
	s.malware = malware
	s.moderator = moderator
}

func (s *Service) SetProcessingPolicy(p ProcessingPolicy) {
	if s == nil {
		return
	}
	s.policy = p
}

func (s *Service) CreatePending(ctx context.Context, ownerUserID ID, originalFilename *string) (Asset, UploadTarget, error) {
	if s == nil || s.store == nil {
		return Asset{}, UploadTarget{}, errStoreRequired
	}
	if s.objects == nil {
		return Asset{}, UploadTarget{}, errStorageRequired
	}
	asset, err := NewPendingAsset(ownerUserID, originalFilename, s.now().UTC())
	if err != nil {
		return Asset{}, UploadTarget{}, err
	}
	if err := s.store.Create(ctx, asset); err != nil {
		return Asset{}, UploadTarget{}, mapStoreErr(err)
	}
	target, err := s.objects.IssueUploadTarget(ctx, asset.ObjectKey)
	if err != nil {
		return Asset{}, UploadTarget{}, mapStoreErr(err)
	}
	if target.ObjectKey != asset.ObjectKey {
		return Asset{}, UploadTarget{}, errInvalidObjectKey
	}
	return asset, target, nil
}

func (s *Service) Get(ctx context.Context, id, ownerUserID ID) (Asset, error) {
	asset, err := s.loadOwned(ctx, id, ownerUserID)
	if err != nil {
		return Asset{}, err
	}
	return asset, nil
}

func (s *Service) ListListingMedia(ctx context.Context, listingID ID) ([]Asset, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if listingID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListByListing(ctx, listingID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	out := make([]Asset, 0, len(rows))
	for _, a := range rows {
		if a.UsableAsListingMedia() {
			out = append(out, a)
		}
	}
	return out, nil
}

// PublicMedia is public-safe processed listing media. Object keys stay out of this type.
type PublicMedia struct {
	AssetID ID
	Kind    Kind
	Order   int
	URL     string
	Width   *int
	Height  *int
}

func (s *Service) ListPublicListingMedia(ctx context.Context, listingID ID) ([]PublicMedia, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if listingID.IsZero() {
		return nil, errZeroID
	}
	if s.objects == nil {
		return []PublicMedia{}, nil
	}
	rows, err := s.store.ListByListing(ctx, listingID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	out := make([]PublicMedia, 0, len(rows))
	for _, a := range rows {
		if !a.UsableAsListingMedia() {
			continue
		}
		if a.ProcessedObjectKey == "" || !IsProcessedObjectKey(a.ProcessedObjectKey) {
			continue
		}
		target, err := s.objects.IssueGetTarget(ctx, a.ProcessedObjectKey)
		if err != nil {
			if errors.Is(err, errInvalidObjectKey) {
				continue
			}
			return nil, mapStoreErr(err)
		}
		if target.URL == "" {
			continue
		}
		item := PublicMedia{
			AssetID: a.ID,
			Kind:    a.Kind,
			URL:     target.URL,
			Width:   cloneInt(a.Width),
			Height:  cloneInt(a.Height),
			Order:   len(out),
		}
		if a.SortOrder != nil {
			item.Order = *a.SortOrder
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) MarkUploaded(ctx context.Context, id, ownerUserID ID, expectedUpdatedAt time.Time) (Asset, error) {
	return s.mutate(ctx, id, ownerUserID, expectedUpdatedAt, func(asset Asset, now time.Time) (Asset, error) {
		stat, err := s.objects.Stat(ctx, asset.ObjectKey)
		if err != nil {
			return Asset{}, mapStoreErr(err)
		}
		if !stat.Exists {
			return Asset{}, errObjectMissing
		}
		return asset.MarkUploaded(now)
	})
}

// ConfirmUpload stats the server-owned object key and marks uploaded, or rejects
// oversized bytes. It does not decode, scan, strip EXIF, resize, or mark ready.
func (s *Service) ConfirmUpload(ctx context.Context, id, ownerUserID ID) (Asset, error) {
	if s.objects == nil {
		return Asset{}, errStorageRequired
	}
	current, err := s.loadOwned(ctx, id, ownerUserID)
	if err != nil {
		return Asset{}, err
	}
	stat, err := s.objects.Stat(ctx, current.ObjectKey)
	if err != nil {
		return Asset{}, mapStoreErr(err)
	}
	if current.Status == StatusUploaded || current.Status == StatusProcessing {
		return current, nil
	}
	if current.Status != StatusPendingUpload {
		return Asset{}, errInvalidTransition
	}
	if !stat.Exists {
		return Asset{}, errObjectMissing
	}
	if stat.SizeBytes < 0 || (s.maxUploadBytes > 0 && stat.SizeBytes > s.maxUploadBytes) {
		now := s.now().UTC()
		rejected, rerr := current.Reject(now)
		if rerr != nil {
			return Asset{}, rerr
		}
		if err := s.store.Update(ctx, rejected, current.UpdatedAt); err != nil {
			return Asset{}, mapStoreErr(err)
		}
		_ = s.objects.DeleteObject(ctx, current.ObjectKey)
		return rejected, errObjectTooLarge
	}
	now := s.now().UTC()
	next, err := current.MarkUploaded(now)
	if err != nil {
		return Asset{}, err
	}
	if err := s.commitUploaded(ctx, current, next); err != nil {
		return Asset{}, err
	}
	return next, nil
}

func (s *Service) commitUploaded(ctx context.Context, current, next Asset) error {
	event, err := encodeProcessEvent(next.ID)
	if err != nil {
		return err
	}
	if s.tx != nil && s.outbox != nil {
		tx, err := s.tx.Begin(ctx)
		if err != nil {
			return mapStoreErr(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := s.store.UpdateInTx(ctx, tx, next, current.UpdatedAt); err != nil {
			return mapStoreErr(err)
		}
		if _, err := s.outbox.Enqueue(ctx, tx, event); err != nil {
			return mapStoreErr(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return mapStoreErr(err)
		}
		return nil
	}
	if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
		return mapStoreErr(err)
	}
	if s.outbox != nil {
		if _, err := s.outbox.Enqueue(ctx, nil, event); err != nil {
			return mapStoreErr(err)
		}
	}
	return nil
}

func (s *Service) MarkProcessing(ctx context.Context, id, ownerUserID ID, expectedUpdatedAt time.Time) (Asset, error) {
	return s.mutate(ctx, id, ownerUserID, expectedUpdatedAt, func(asset Asset, now time.Time) (Asset, error) {
		return asset.MarkProcessing(now)
	})
}

func (s *Service) MarkReady(ctx context.Context, id, ownerUserID ID, expectedUpdatedAt time.Time, meta ValidatedMetadata) (Asset, error) {
	return s.mutate(ctx, id, ownerUserID, expectedUpdatedAt, func(asset Asset, now time.Time) (Asset, error) {
		processed, err := ProcessedObjectKeyFromOriginal(asset.OwnerUserID, asset.ID, asset.ObjectKey)
		if err != nil {
			return Asset{}, err
		}
		return asset.MarkReady(meta, processed, now)
	})
}

func (s *Service) Reject(ctx context.Context, id, ownerUserID ID, expectedUpdatedAt time.Time) (Asset, error) {
	return s.mutate(ctx, id, ownerUserID, expectedUpdatedAt, func(asset Asset, now time.Time) (Asset, error) {
		return asset.Reject(now)
	})
}

func (s *Service) Delete(ctx context.Context, id, ownerUserID ID, expectedUpdatedAt time.Time) (Asset, error) {
	asset, err := s.mutate(ctx, id, ownerUserID, expectedUpdatedAt, func(asset Asset, now time.Time) (Asset, error) {
		return asset.Delete(now)
	})
	if err != nil {
		return Asset{}, err
	}
	if err := s.objects.DeleteObject(ctx, asset.ObjectKey); err != nil {
		return Asset{}, mapStoreErr(err)
	}
	if asset.ProcessedObjectKey != "" {
		if err := s.objects.DeleteObject(ctx, asset.ProcessedObjectKey); err != nil {
			return Asset{}, mapStoreErr(err)
		}
	}
	return asset, nil
}

func (s *Service) AttachListing(ctx context.Context, id ID, bind ListingBind, expectedUpdatedAt time.Time) (Asset, error) {
	if err := bind.validate(); err != nil {
		return Asset{}, err
	}
	return s.mutate(ctx, id, bind.ActorUserID, expectedUpdatedAt, func(asset Asset, now time.Time) (Asset, error) {
		return asset.AttachListing(bind.ListingID, now)
	})
}

func (s *Service) AssertAttachable(ctx context.Context, id, ownerUserID ID) error {
	asset, err := s.loadOwned(ctx, id, ownerUserID)
	if err != nil {
		return err
	}
	if asset.Status == StatusRejected || asset.Status == StatusDeleted {
		return errNotUsable
	}
	if asset.ListingID != nil {
		return errAlreadyAttached
	}
	return nil
}

// ListingBind is caller-provided listing ownership context. Media does not query Listings.
type ListingBind struct {
	ListingID   ID
	ActorUserID ID
}

func (b ListingBind) validate() error {
	if b.ListingID.IsZero() || b.ActorUserID.IsZero() {
		return errZeroID
	}
	return nil
}

func (s *Service) DetachListing(ctx context.Context, id, ownerUserID ID, expectedUpdatedAt time.Time) (Asset, error) {
	return s.mutate(ctx, id, ownerUserID, expectedUpdatedAt, func(asset Asset, now time.Time) (Asset, error) {
		return asset.DetachListing(now)
	})
}

func (s *Service) OrderListingImages(ctx context.Context, ownerUserID, listingID ID, orderedIDs []ID) ([]Asset, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if ownerUserID.IsZero() || listingID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListByListing(ctx, listingID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	ready := make([]Asset, 0)
	byID := make(map[ID]Asset)
	for _, a := range rows {
		if a.OwnerUserID != ownerUserID {
			return nil, errForbidden
		}
		if !a.UsableAsListingMedia() {
			continue
		}
		ready = append(ready, a)
		byID[a.ID] = a
	}
	if len(orderedIDs) != len(ready) {
		return nil, errInvalidOrder
	}
	seen := make(map[ID]struct{}, len(orderedIDs))
	now := s.now().UTC()
	out := make([]Asset, 0, len(orderedIDs))
	for i, id := range orderedIDs {
		if _, dup := seen[id]; dup {
			return nil, errInvalidOrder
		}
		seen[id] = struct{}{}
		current, ok := byID[id]
		if !ok {
			return nil, errInvalidOrder
		}
		next, err := current.WithSortOrder(i, now)
		if err != nil {
			return nil, err
		}
		if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
			return nil, mapStoreErr(err)
		}
		out = append(out, next)
	}
	return out, nil
}

func (s *Service) loadOwned(ctx context.Context, id, ownerUserID ID) (Asset, error) {
	if s == nil || s.store == nil {
		return Asset{}, errStoreRequired
	}
	if id.IsZero() || ownerUserID.IsZero() {
		return Asset{}, errZeroID
	}
	asset, err := s.store.Get(ctx, id)
	if err != nil {
		return Asset{}, mapStoreErr(err)
	}
	if asset.OwnerUserID != ownerUserID {
		return Asset{}, errForbidden
	}
	return asset, nil
}

func (s *Service) mutate(ctx context.Context, id, ownerUserID ID, expectedUpdatedAt time.Time, fn func(Asset, time.Time) (Asset, error)) (Asset, error) {
	if s.objects == nil {
		return Asset{}, errStorageRequired
	}
	current, err := s.loadOwned(ctx, id, ownerUserID)
	if err != nil {
		return Asset{}, err
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return Asset{}, errConflict
	}
	next, err := fn(current, s.now().UTC())
	if err != nil {
		return Asset{}, err
	}
	if err := s.store.Update(ctx, next, expectedUpdatedAt); err != nil {
		return Asset{}, mapStoreErr(err)
	}
	return next, nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, errConflict) || errors.Is(err, db.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, db.ErrUnavailable) {
		return errUnavailable
	}
	if errors.Is(err, errStoreRequired) || errors.Is(err, errStorageRequired) ||
		errors.Is(err, errZeroID) || errors.Is(err, errInvalidAsset) ||
		errors.Is(err, errInvalidKind) || errors.Is(err, errInvalidStatus) ||
		errors.Is(err, errInvalidTransition) || errors.Is(err, errInvalidMetadata) ||
		errors.Is(err, errInvalidObjectKey) || errors.Is(err, errInvalidFilename) ||
		errors.Is(err, errNotUsable) || errors.Is(err, errAlreadyAttached) ||
		errors.Is(err, errNotAttached) || errors.Is(err, errForbidden) ||
		errors.Is(err, errObjectMissing) || errors.Is(err, errObjectTooLarge) ||
		errors.Is(err, errInvalidOrder) || errors.Is(err, errUnsupportedImage) ||
		errors.Is(err, errInvalidImage) || errors.Is(err, errScannerUnavailable) ||
		errors.Is(err, errModeratorUnavailable) ||
		errors.Is(err, outbox.ErrSensitivePayload) || errors.Is(err, outbox.ErrInvalidEvent) ||
		errors.Is(err, outbox.ErrConflict) {
		return err
	}
	return errUnavailable
}
