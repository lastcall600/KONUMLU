package reviewaggregates

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is an in-process derived projection for tests.
type MemoryStore struct {
	mu        sync.Mutex
	processed map[ID]struct{}
	listings  map[ID]RatingSummary
	providers map[ID]RatingSummary
	fail      error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		processed: make(map[ID]struct{}),
		listings:  make(map[ID]RatingSummary),
		providers: make(map[ID]RatingSummary),
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

func (m *MemoryStore) ApplyVerified(ctx context.Context, event VerifiedReview, now time.Time) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.processed[event.EventID]; ok {
		return nil
	}
	m.processed[event.EventID] = struct{}{}
	if err := m.bumpLocked(m.listings, event.ListingID, event.ListingAccuracy, now); err != nil {
		return err
	}
	return m.bumpLocked(m.providers, event.ProviderUserID, event.ProviderService, now)
}

func (m *MemoryStore) bumpLocked(dest map[ID]RatingSummary, key ID, rating int, now time.Time) error {
	row, ok := dest[key]
	if !ok {
		row = RatingSummary{}
	}
	row.ReviewCount++
	row.RatingSum += rating
	row.UpdatedAt = now.UTC()
	if err := row.Validate(); err != nil {
		return err
	}
	dest[key] = row
	return nil
}

func (m *MemoryStore) GetListingAccuracy(ctx context.Context, listingID ID) (RatingSummary, error) {
	return m.get(ctx, m.listings, listingID)
}

func (m *MemoryStore) GetProviderService(ctx context.Context, providerUserID ID) (RatingSummary, error) {
	return m.get(ctx, m.providers, providerUserID)
}

func (m *MemoryStore) get(ctx context.Context, src map[ID]RatingSummary, id ID) (RatingSummary, error) {
	if m == nil {
		return RatingSummary{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return RatingSummary{}, err
	}
	if id.IsZero() {
		return RatingSummary{}, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return RatingSummary{}, m.fail
	}
	row, ok := src[id]
	if !ok {
		return RatingSummary{}, errNotFound
	}
	return row, nil
}

var _ projectionStore = (*MemoryStore)(nil)
