package deliveries

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu    sync.Mutex
	byID  map[ID]Delivery
	byTxn map[ID]ID
	fail  error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:  make(map[ID]Delivery),
		byTxn: make(map[ID]ID),
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

func (m *MemoryStore) Create(ctx context.Context, d Delivery) error {
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
	if err := d.Validate(); err != nil {
		return err
	}
	if _, ok := m.byID[d.ID]; ok {
		return errConflict
	}
	if _, ok := m.byTxn[d.TransactionID]; ok {
		return errConflict
	}
	m.byID[d.ID] = cloneDelivery(d)
	m.byTxn[d.TransactionID] = d.ID
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, id ID) (Delivery, error) {
	if m == nil {
		return Delivery{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Delivery{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Delivery{}, m.fail
	}
	d, ok := m.byID[id]
	if !ok {
		return Delivery{}, errNotFound
	}
	return cloneDelivery(d), nil
}

func (m *MemoryStore) GetByTransactionID(ctx context.Context, transactionID ID) (Delivery, error) {
	if m == nil {
		return Delivery{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Delivery{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Delivery{}, m.fail
	}
	id, ok := m.byTxn[transactionID]
	if !ok {
		return Delivery{}, errNotFound
	}
	return cloneDelivery(m.byID[id]), nil
}

func (m *MemoryStore) ListForParticipant(ctx context.Context, userID ID) ([]Delivery, error) {
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
	out := make([]Delivery, 0)
	for _, d := range m.byID {
		if d.Participant(userID) {
			out = append(out, cloneDelivery(d))
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

func (m *MemoryStore) Update(ctx context.Context, d Delivery, expectedUpdatedAt time.Time) error {
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
	if err := d.Validate(); err != nil {
		return err
	}
	current, ok := m.byID[d.ID]
	if !ok {
		return errNotFound
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	m.byID[d.ID] = cloneDelivery(d)
	return nil
}

var _ store = (*MemoryStore)(nil)
