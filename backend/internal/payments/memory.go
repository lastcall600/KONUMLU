package payments

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu         sync.Mutex
	byID       map[ID]Payment
	byTxn      map[ID]ID
	byProvider map[string]ID
	fail       error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:       make(map[ID]Payment),
		byTxn:      make(map[ID]ID),
		byProvider: make(map[string]ID),
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

func (m *MemoryStore) Create(ctx context.Context, p Payment) error {
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
	if err := p.Validate(); err != nil {
		return err
	}
	if _, ok := m.byID[p.ID]; ok {
		return errConflict
	}
	if _, ok := m.byTxn[p.TransactionID]; ok {
		return errConflict
	}
	if p.ProviderReference != nil {
		if _, ok := m.byProvider[*p.ProviderReference]; ok {
			return errConflict
		}
		m.byProvider[*p.ProviderReference] = p.ID
	}
	m.byID[p.ID] = clonePayment(p)
	m.byTxn[p.TransactionID] = p.ID
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, id ID) (Payment, error) {
	if m == nil {
		return Payment{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Payment{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Payment{}, m.fail
	}
	p, ok := m.byID[id]
	if !ok {
		return Payment{}, errNotFound
	}
	return clonePayment(p), nil
}

func (m *MemoryStore) GetByTransactionID(ctx context.Context, transactionID ID) (Payment, error) {
	if m == nil {
		return Payment{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Payment{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Payment{}, m.fail
	}
	id, ok := m.byTxn[transactionID]
	if !ok {
		return Payment{}, errNotFound
	}
	return clonePayment(m.byID[id]), nil
}

func (m *MemoryStore) ListForParticipant(ctx context.Context, userID ID) ([]Payment, error) {
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
	out := make([]Payment, 0)
	for _, p := range m.byID {
		if p.Participant(userID) {
			out = append(out, clonePayment(p))
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

func (m *MemoryStore) Update(ctx context.Context, p Payment, expectedUpdatedAt time.Time) error {
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
	if err := p.Validate(); err != nil {
		return err
	}
	current, ok := m.byID[p.ID]
	if !ok {
		return errNotFound
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	if current.ProviderReference != nil {
		delete(m.byProvider, *current.ProviderReference)
	}
	if p.ProviderReference != nil {
		if owner, ok := m.byProvider[*p.ProviderReference]; ok && owner != p.ID {
			return errConflict
		}
		m.byProvider[*p.ProviderReference] = p.ID
	}
	m.byID[p.ID] = clonePayment(p)
	return nil
}

var _ store = (*MemoryStore)(nil)
