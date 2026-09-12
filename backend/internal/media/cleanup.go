package media

import (
	"context"
	"time"

	"backend/internal/platform/observability"
)

const (
	defaultOrphanSweepEvery = 5 * time.Minute
	defaultPendingOrphanAge = 24 * time.Hour
	defaultRejectedOrphanAge = 24 * time.Hour
	defaultOrphanSweepLimit = 50
)

// SweepOrphans deletes expired pending uploads and rejected quarantine objects.
// Ready/approved media is never selected.
func (s *Service) SweepOrphans(ctx context.Context) (int, error) {
	if s == nil || s.store == nil {
		return 0, errStoreRequired
	}
	if s.objects == nil {
		return 0, errStorageRequired
	}
	now := s.now().UTC()
	rows, err := s.store.ListReclaimable(ctx, now, defaultPendingOrphanAge, defaultRejectedOrphanAge, defaultOrphanSweepLimit)
	if err != nil {
		return 0, mapStoreErr(err)
	}
	n := 0
	for _, current := range rows {
		if current.Status == StatusReady {
			continue
		}
		if current.Status != StatusPendingUpload && current.Status != StatusRejected {
			continue
		}
		if err := s.objects.DeleteObject(ctx, current.ObjectKey); err != nil {
			return n, mapStoreErr(err)
		}
		if current.ProcessedObjectKey != "" {
			if err := s.objects.DeleteObject(ctx, current.ProcessedObjectKey); err != nil {
				return n, mapStoreErr(err)
			}
		}
		next, err := current.Delete(now)
		if err != nil {
			return n, err
		}
		if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
			return n, mapStoreErr(err)
		}
		n++
		observability.FromContext(ctx).Info("media_orphan_reclaim",
			"media_id", current.ID.String(),
			"media_status", string(current.Status),
			"processing_outcome", "orphan_reclaimed",
			"object_category", "listing-images",
		)
	}
	return n, nil
}

// RunOrphanSweeper periodically reclaims expired untrusted media. It never
// deletes ready objects. Storage/DB errors are logged and retried next tick.
func RunOrphanSweeper(ctx context.Context, svc *Service, every time.Duration) {
	if svc == nil {
		return
	}
	if every <= 0 {
		every = defaultOrphanSweepEvery
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := svc.SweepOrphans(ctx); err != nil {
				observability.FromContext(ctx).Info("media_orphan_reclaim",
					"processing_outcome", "retry",
					"object_category", "listing-images",
					"error_class", processErrorClass(err),
				)
			}
		}
	}
}
