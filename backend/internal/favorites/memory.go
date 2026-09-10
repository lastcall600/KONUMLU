package favorites

import (
	"context"
	"sort"
	"sync"
)

type memoryKey struct {
	user    ID
	listing ID
}

// MemoryStore is an in-process favorite store for tests.
type MemoryStore struct {
	mu   sync.Mutex
	byKey map[memoryKey]Favorite
	fail error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byKey: make(map[memoryKey]Favorite)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) Add(ctx context.Context, fav Favorite) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fav.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	key := memoryKey{user: fav.UserID, listing: fav.ListingID}
	if _, ok := m.byKey[key]; ok {
		return nil
	}
	m.byKey[key] = Favorite{UserID: fav.UserID, ListingID: fav.ListingID, CreatedAt: fav.CreatedAt.UTC()}
	return nil
}

func (m *MemoryStore) Remove(ctx context.Context, userID, listingID ID) error {
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
	delete(m.byKey, memoryKey{user: userID, listing: listingID})
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, userID, listingID ID) (Favorite, error) {
	if m == nil {
		return Favorite{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Favorite{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Favorite{}, m.fail
	}
	fav, ok := m.byKey[memoryKey{user: userID, listing: listingID}]
	if !ok {
		return Favorite{}, errNotFound
	}
	return fav, nil
}

func (m *MemoryStore) ListByUser(ctx context.Context, userID ID) ([]Favorite, error) {
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
	out := make([]Favorite, 0)
	for _, fav := range m.byKey {
		if fav.UserID == userID {
			out = append(out, fav)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ListingID.String() < out[j].ListingID.String()
	})
	return out, nil
}
