package reviews

import (
	"context"
	"sort"
	"sync"

	"backend/internal/platform/outbox"
)

var _ reviewStore = (*MemoryStore)(nil)

type MemoryStore struct {
	mu            sync.Mutex
	byID          map[ID]Review
	byInteraction map[ID]ID
	fail          error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:          make(map[ID]Review),
		byInteraction: make(map[ID]ID),
	}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) InsertReview(ctx context.Context, row Review, enqueue func(ctx context.Context, exec outbox.Execer) error) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := row.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.fail != nil {
		err := m.fail
		m.mu.Unlock()
		return err
	}
	if _, ok := m.byInteraction[row.VerifiedInteractionID]; ok {
		m.mu.Unlock()
		return errConflict
	}
	if _, ok := m.byID[row.ID]; ok {
		m.mu.Unlock()
		return errConflict
	}
	cloned := cloneReview(row)
	m.byID[row.ID] = cloned
	m.byInteraction[row.VerifiedInteractionID] = row.ID
	m.mu.Unlock()
	if enqueue != nil {
		if err := enqueue(ctx, nil); err != nil {
			m.mu.Lock()
			delete(m.byID, row.ID)
			delete(m.byInteraction, row.VerifiedInteractionID)
			m.mu.Unlock()
			return err
		}
	}
	return nil
}

func (m *MemoryStore) GetByInteraction(ctx context.Context, verifiedInteractionID ID) (Review, error) {
	if m == nil {
		return Review{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Review{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Review{}, m.fail
	}
	id, ok := m.byInteraction[verifiedInteractionID]
	if !ok {
		return Review{}, errNotFound
	}
	return cloneReview(m.byID[id]), nil
}

func (m *MemoryStore) ListForReviewer(ctx context.Context, reviewerUserID ID, limit int) ([]Review, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if limit <= 0 {
		limit = MaxReviews
	}
	out := make([]Review, 0)
	for _, row := range m.byID {
		if row.ReviewerUserID == reviewerUserID {
			out = append(out, cloneReview(row))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID.String() > out[j].ID.String()
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) ListForListing(ctx context.Context, listingID ID, cursor *listingCursor, limit int) ([]Review, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if listingID.IsZero() {
		return nil, errZeroID
	}
	if limit <= 0 {
		limit = MaxPublicReviews
	}
	out := make([]Review, 0)
	for _, row := range m.byID {
		if row.ListingID != listingID {
			continue
		}
		if !afterListingCursor(row, cursor) {
			continue
		}
		out = append(out, cloneReview(row))
	}
	sort.Slice(out, func(i, j int) bool {
		return compareListingReviews(out[i], out[j])
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func cloneReview(row Review) Review {
	out := row
	if row.Body != nil {
		v := *row.Body
		out.Body = &v
	}
	return out
}
