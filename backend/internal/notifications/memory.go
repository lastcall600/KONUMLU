package notifications

import (
	"context"
	"sync"
)

// MemoryStore is an in-process delivery store for tests and worker wiring tests.
type MemoryStore struct {
	mu       sync.Mutex
	byIntent map[ID]Delivery
	byID     map[ID]Delivery
	warnings map[ID]WarningRecord
	fail     error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byIntent: make(map[ID]Delivery),
		byID:     make(map[ID]Delivery),
		warnings: make(map[ID]WarningRecord),
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

func (m *MemoryStore) UpsertDelivery(ctx context.Context, delivery Delivery) (Delivery, error) {
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
	if err := delivery.Validate(); err != nil {
		return Delivery{}, err
	}
	if existing, ok := m.byIntent[delivery.IntentID]; ok {
		existing.UpdatedAt = delivery.UpdatedAt
		if delivery.CorrelationID != nil {
			existing.CorrelationID = delivery.CorrelationID
		}
		m.byIntent[delivery.IntentID] = existing
		m.byID[existing.ID] = existing
		return existing, nil
	}
	m.byIntent[delivery.IntentID] = delivery
	m.byID[delivery.ID] = delivery
	return delivery, nil
}

func (m *MemoryStore) SaveDelivery(ctx context.Context, delivery Delivery) (Delivery, error) {
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
	if err := delivery.Validate(); err != nil {
		return Delivery{}, err
	}
	existing, ok := m.byID[delivery.ID]
	if !ok {
		existing, ok = m.byIntent[delivery.IntentID]
	}
	if !ok {
		return Delivery{}, errInvalidDelivery
	}
	existing.Status = delivery.Status
	existing.Attempts = delivery.Attempts
	existing.ProviderRef = delivery.ProviderRef
	existing.UpdatedAt = delivery.UpdatedAt
	existing.LastAttemptAt = delivery.LastAttemptAt
	existing.CompletedAt = delivery.CompletedAt
	if err := existing.Validate(); err != nil {
		return Delivery{}, err
	}
	m.byIntent[existing.IntentID] = existing
	m.byID[existing.ID] = existing
	return existing, nil
}

func (m *MemoryStore) GetByIntentID(id ID) (Delivery, bool) {
	if m == nil {
		return Delivery{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.byIntent[id]
	return d, ok
}

func (m *MemoryStore) GetByID(id ID) (Delivery, bool) {
	if m == nil {
		return Delivery{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.byID[id]
	return d, ok
}

func (m *MemoryStore) UpsertWarning(ctx context.Context, row WarningRecord) (WarningRecord, error) {
	if m == nil {
		return WarningRecord{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return WarningRecord{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return WarningRecord{}, m.fail
	}
	if err := row.Validate(); err != nil {
		return WarningRecord{}, err
	}
	if existing, ok := m.warnings[row.IntentID]; ok {
		existing.UpdatedAt = row.UpdatedAt
		m.warnings[row.IntentID] = existing
		return existing, nil
	}
	m.warnings[row.IntentID] = row
	return row, nil
}

func (m *MemoryStore) GetWarning(id ID) (WarningRecord, bool) {
	if m == nil {
		return WarningRecord{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.warnings[id]
	return w, ok
}

func (m *MemoryStore) WarningLen() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.warnings)
}

func (m *MemoryStore) Len() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byIntent)
}

var _ deliveryStore = (*MemoryStore)(nil)
