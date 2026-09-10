package needs

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu   sync.Mutex
	byID map[ID]Need
	fail error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[ID]Need)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) Create(ctx context.Context, need Need) error {
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
	if err := need.Validate(); err != nil {
		return err
	}
	if _, ok := m.byID[need.ID]; ok {
		return errConflict
	}
	m.byID[need.ID] = cloneNeed(need)
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, id ID) (Need, error) {
	if m == nil {
		return Need{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Need{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Need{}, m.fail
	}
	need, ok := m.byID[id]
	if !ok {
		return Need{}, errNotFound
	}
	return cloneNeed(need), nil
}

func (m *MemoryStore) ListByRequester(ctx context.Context, requesterUserID ID) ([]Need, error) {
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
	out := make([]Need, 0)
	for _, need := range m.byID {
		if need.RequesterUserID == requesterUserID {
			out = append(out, cloneNeed(need))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return bytes.Compare(out[i].ID[:], out[j].ID[:]) < 0
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (m *MemoryStore) Update(ctx context.Context, need Need, expectedUpdatedAt time.Time) error {
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
	if err := need.Validate(); err != nil {
		return err
	}
	current, ok := m.byID[need.ID]
	if !ok {
		return errNotFound
	}
	if current.RequesterUserID != need.RequesterUserID {
		return errConflict
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	m.byID[need.ID] = cloneNeed(need)
	return nil
}

var _ store = (*MemoryStore)(nil)
