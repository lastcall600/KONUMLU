package disputes

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu       sync.Mutex
	byID     map[ID]Dispute
	evidence map[ID][]Evidence
	fail     error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:     make(map[ID]Dispute),
		evidence: make(map[ID][]Evidence),
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

func (m *MemoryStore) Create(ctx context.Context, d Dispute) error {
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
	if d.Status.Active() {
		for _, existing := range m.byID {
			if existing.TransactionID == d.TransactionID && existing.Status.Active() {
				return errConflict
			}
		}
	}
	m.byID[d.ID] = cloneDispute(d)
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, id ID) (Dispute, error) {
	if m == nil {
		return Dispute{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Dispute{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Dispute{}, m.fail
	}
	d, ok := m.byID[id]
	if !ok {
		return Dispute{}, errNotFound
	}
	return cloneDispute(d), nil
}

func (m *MemoryStore) GetActiveByTransactionID(ctx context.Context, transactionID ID) (Dispute, error) {
	if m == nil {
		return Dispute{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Dispute{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Dispute{}, m.fail
	}
	for _, d := range m.byID {
		if d.TransactionID == transactionID && d.Status.Active() {
			return cloneDispute(d), nil
		}
	}
	return Dispute{}, errNotFound
}

func (m *MemoryStore) ListConcludedByTransactionID(ctx context.Context, transactionID ID) ([]Dispute, error) {
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
	out := make([]Dispute, 0)
	for _, d := range m.byID {
		if d.TransactionID == transactionID && d.Status.Concluded() {
			out = append(out, cloneDispute(d))
		}
	}
	return out, nil
}

func (m *MemoryStore) ListForParticipant(ctx context.Context, userID ID) ([]Dispute, error) {
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
	out := make([]Dispute, 0)
	for _, d := range m.byID {
		if d.Participant(userID) {
			out = append(out, cloneDispute(d))
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

func (m *MemoryStore) Update(ctx context.Context, d Dispute, expectedUpdatedAt time.Time) error {
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
	m.byID[d.ID] = cloneDispute(d)
	return nil
}

func (m *MemoryStore) AppendEvidence(ctx context.Context, e Evidence) error {
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
	if err := e.Validate(); err != nil {
		return err
	}
	if _, ok := m.byID[e.DisputeID]; !ok {
		return errNotFound
	}
	for _, existing := range m.evidence[e.DisputeID] {
		if existing.ID == e.ID {
			return errConflict
		}
	}
	m.evidence[e.DisputeID] = append(m.evidence[e.DisputeID], cloneEvidence(e))
	return nil
}

func (m *MemoryStore) ListEvidence(ctx context.Context, disputeID ID) ([]Evidence, error) {
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
	list := m.evidence[disputeID]
	out := make([]Evidence, 0, len(list))
	for _, e := range list {
		out = append(out, cloneEvidence(e))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return bytes.Compare(out[i].ID[:], out[j].ID[:]) < 0
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

var _ store = (*MemoryStore)(nil)
