package savedsearch

import (
	"context"
	"sort"
	"sync"
)

var _ savedSearchStore = (*MemoryStore)(nil)

// MemoryStore is an in-process saved-search store for tests.
type MemoryStore struct {
	mu   sync.Mutex
	byID map[ID]SavedSearch
	fail error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[ID]SavedSearch)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) Insert(ctx context.Context, row SavedSearch) error {
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
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.byID[row.ID]; ok {
		return errUnavailable
	}
	m.byID[row.ID] = cloneSaved(row)
	return nil
}

func (m *MemoryStore) GetByUser(ctx context.Context, userID, id ID) (SavedSearch, error) {
	if m == nil {
		return SavedSearch{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return SavedSearch{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return SavedSearch{}, m.fail
	}
	row, ok := m.byID[id]
	if !ok || row.UserID != userID {
		return SavedSearch{}, errNotFound
	}
	return cloneSaved(row), nil
}

func (m *MemoryStore) ListByUser(ctx context.Context, userID ID) ([]SavedSearch, error) {
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
	out := make([]SavedSearch, 0)
	for _, row := range m.byID {
		if row.UserID == userID {
			out = append(out, cloneSaved(row))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out, nil
}

func (m *MemoryStore) DeleteByUser(ctx context.Context, userID, id ID) error {
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
	row, ok := m.byID[id]
	if !ok || row.UserID != userID {
		return errNotFound
	}
	delete(m.byID, id)
	return nil
}

func cloneSaved(row SavedSearch) SavedSearch {
	out := row
	out.Filters = cloneFilters(row.Filters)
	return out
}

func cloneFilters(f Filters) Filters {
	out := Filters{}
	if f.Q != nil {
		q := *f.Q
		out.Q = &q
	}
	if f.CategoryID != nil {
		id := *f.CategoryID
		out.CategoryID = &id
	}
	if f.MinPrice != nil {
		p := *f.MinPrice
		out.MinPrice = &p
	}
	if f.MaxPrice != nil {
		p := *f.MaxPrice
		out.MaxPrice = &p
	}
	if f.Currency != nil {
		c := *f.Currency
		out.Currency = &c
	}
	if f.Viewport != nil {
		v := *f.Viewport
		out.Viewport = &v
	}
	return out
}
