package transactions

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu      sync.Mutex
	byID    map[ID]Transaction
	byOffer map[ID]ID
	fail    error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[ID]Transaction), byOffer: make(map[ID]ID)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) Create(ctx context.Context, txn Transaction) error {
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
	if err := txn.Validate(); err != nil {
		return err
	}
	if _, ok := m.byID[txn.ID]; ok {
		return errConflict
	}
	if _, ok := m.byOffer[txn.OfferID]; ok {
		return errConflict
	}
	m.byID[txn.ID] = cloneTxn(txn)
	m.byOffer[txn.OfferID] = txn.ID
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, id ID) (Transaction, error) {
	if m == nil {
		return Transaction{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Transaction{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Transaction{}, m.fail
	}
	txn, ok := m.byID[id]
	if !ok {
		return Transaction{}, errNotFound
	}
	return cloneTxn(txn), nil
}

func (m *MemoryStore) GetByOfferID(ctx context.Context, offerID ID) (Transaction, error) {
	if m == nil {
		return Transaction{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Transaction{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Transaction{}, m.fail
	}
	id, ok := m.byOffer[offerID]
	if !ok {
		return Transaction{}, errNotFound
	}
	return cloneTxn(m.byID[id]), nil
}

func (m *MemoryStore) ListForParticipant(ctx context.Context, userID ID) ([]Transaction, error) {
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
	out := make([]Transaction, 0)
	for _, txn := range m.byID {
		if txn.Participant(userID) {
			out = append(out, cloneTxn(txn))
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

func (m *MemoryStore) Update(ctx context.Context, txn Transaction, expectedUpdatedAt time.Time) error {
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
	if err := txn.Validate(); err != nil {
		return err
	}
	current, ok := m.byID[txn.ID]
	if !ok {
		return errNotFound
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	m.byID[txn.ID] = cloneTxn(txn)
	return nil
}

var _ store = (*MemoryStore)(nil)
