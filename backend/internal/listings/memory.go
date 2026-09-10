package listings

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryStore is an in-process listing store for tests.
type MemoryStore struct {
	mu   sync.Mutex
	byID map[ID]Listing
	fail error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[ID]Listing)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) Create(ctx context.Context, listing Listing) error {
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
	if err := listing.Validate(); err != nil {
		return err
	}
	if _, ok := m.byID[listing.ID]; ok {
		return errConflict
	}
	m.byID[listing.ID] = cloneListing(listing)
	return nil
}

func (m *MemoryStore) Snapshot() map[ID]Listing {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[ID]Listing, len(m.byID))
	for k, v := range m.byID {
		out[k] = cloneListing(v)
	}
	return out
}

func (m *MemoryStore) Restore(in map[ID]Listing) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byID = make(map[ID]Listing, len(in))
	for k, v := range in {
		m.byID[k] = cloneListing(v)
	}
}

func (m *MemoryStore) Get(ctx context.Context, id ID) (Listing, error) {
	if m == nil {
		return Listing{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Listing{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Listing{}, m.fail
	}
	listing, ok := m.byID[id]
	if !ok {
		return Listing{}, errNotFound
	}
	return cloneListing(listing), nil
}

func (m *MemoryStore) Update(ctx context.Context, listing Listing, expectedUpdatedAt time.Time) error {
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
	if err := listing.Validate(); err != nil {
		return err
	}
	current, ok := m.byID[listing.ID]
	if !ok {
		return errNotFound
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	m.byID[listing.ID] = cloneListing(listing)
	return nil
}

func (m *MemoryStore) ListByOwner(ctx context.Context, ownerUserID ID) ([]Listing, error) {
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
	out := make([]Listing, 0)
	for _, listing := range m.byID {
		if listing.OwnerUserID == ownerUserID {
			out = append(out, cloneListing(listing))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID.String() < out[j].ID.String()
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func cloneListing(l Listing) Listing {
	l.Attributes = cloneAttributes(l.Attributes)
	l.PriceAmount, l.PriceCurrency = clonePrice(l.PriceAmount, l.PriceCurrency)
	if l.PublishedAt != nil {
		t := *l.PublishedAt
		l.PublishedAt = &t
	}
	if l.ArchivedAt != nil {
		t := *l.ArchivedAt
		l.ArchivedAt = &t
	}
	return l
}

var _ listingStore = (*MemoryStore)(nil)
