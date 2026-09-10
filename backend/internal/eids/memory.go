package eids

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu   sync.Mutex
	byID map[ID]Verification
	fail error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[ID]Verification)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) lockedErr(ctx context.Context) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.fail != nil {
		return m.fail
	}
	return nil
}

func (m *MemoryStore) Create(ctx context.Context, v Verification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := v.Validate(); err != nil {
		return err
	}
	if _, ok := m.byID[v.ID]; ok {
		return errConflict
	}
	if v.Status.Open() {
		for _, existing := range m.byID {
			if existing.ListingID == v.ListingID && existing.VerificationType == v.VerificationType && existing.Status.Open() {
				return errConflict
			}
		}
	}
	m.byID[v.ID] = cloneVerification(v)
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, id ID) (Verification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return Verification{}, err
	}
	v, ok := m.byID[id]
	if !ok {
		return Verification{}, errNotFound
	}
	return cloneVerification(v), nil
}

func (m *MemoryStore) GetOpen(ctx context.Context, listingID ID, kind VerificationType) (Verification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return Verification{}, err
	}
	var found *Verification
	for _, v := range m.byID {
		if v.ListingID == listingID && v.VerificationType == kind && v.Status.Open() {
			cp := cloneVerification(v)
			if found == nil || cp.UpdatedAt.After(found.UpdatedAt) {
				found = &cp
			}
		}
	}
	if found == nil {
		return Verification{}, errNotFound
	}
	return *found, nil
}

func (m *MemoryStore) GetLatest(ctx context.Context, listingID ID, kind VerificationType) (Verification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return Verification{}, err
	}
	var found *Verification
	for _, v := range m.byID {
		if v.ListingID != listingID || v.VerificationType != kind {
			continue
		}
		cp := cloneVerification(v)
		if found == nil || cp.UpdatedAt.After(found.UpdatedAt) || (cp.UpdatedAt.Equal(found.UpdatedAt) && bytesGreater(cp.ID, found.ID)) {
			found = &cp
		}
	}
	if found == nil {
		return Verification{}, errNotFound
	}
	return *found, nil
}

func bytesGreater(a, b ID) bool {
	for i := range a {
		if a[i] > b[i] {
			return true
		}
		if a[i] < b[i] {
			return false
		}
	}
	return false
}

func (m *MemoryStore) Update(ctx context.Context, v Verification, expectedUpdatedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if err := v.Validate(); err != nil {
		return err
	}
	cur, ok := m.byID[v.ID]
	if !ok {
		return errNotFound
	}
	if !cur.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	if v.Status.Open() {
		for id, existing := range m.byID {
			if id == v.ID {
				continue
			}
			if existing.ListingID == v.ListingID && existing.VerificationType == v.VerificationType && existing.Status.Open() {
				return errConflict
			}
		}
	}
	m.byID[v.ID] = cloneVerification(v)
	return nil
}
