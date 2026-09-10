package location

import (
	"context"
	"sync"
)

// MemoryStore is an in-process listing-location store for tests. It does not use PostGIS.
type MemoryStore struct {
	mu        sync.Mutex
	byListing map[ID]ListingLocation
	fail      error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byListing: make(map[ID]ListingLocation)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) Upsert(ctx context.Context, loc ListingLocation) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if err := loc.Validate(); err != nil {
		return err
	}
	if existing, ok := m.byListing[loc.ListingID]; ok {
		loc.CreatedAt = existing.CreatedAt
		if loc.UpdatedAt.Before(loc.CreatedAt) {
			return errInvalidLocation
		}
	}
	m.byListing[loc.ListingID] = cloneListingLocation(loc)
	return nil
}

func (m *MemoryStore) Snapshot() map[ID]ListingLocation {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[ID]ListingLocation, len(m.byListing))
	for k, v := range m.byListing {
		out[k] = cloneListingLocation(v)
	}
	return out
}

func (m *MemoryStore) Restore(in map[ID]ListingLocation) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byListing = make(map[ID]ListingLocation, len(in))
	for k, v := range in {
		m.byListing[k] = cloneListingLocation(v)
	}
}

func (m *MemoryStore) GetByListingID(ctx context.Context, listingID ID) (ListingLocation, error) {
	if m == nil {
		return ListingLocation{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return ListingLocation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return ListingLocation{}, m.fail
	}
	loc, ok := m.byListing[listingID]
	if !ok {
		return ListingLocation{}, errNotFound
	}
	return cloneListingLocation(loc), nil
}

func (m *MemoryStore) DeleteByListingID(ctx context.Context, listingID ID) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	delete(m.byListing, listingID)
	return nil
}

var _ listingLocationStore = (*MemoryStore)(nil)
