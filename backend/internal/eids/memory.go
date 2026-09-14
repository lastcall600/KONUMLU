package eids

import (
	"context"
	"sync"
	"time"
)

var _ store = (*MemoryStore)(nil)

type MemoryStore struct {
	mu      sync.Mutex
	byID    map[ID]Verification
	subjects map[string]SubjectBinding
	byVer   map[ID]string
	replays map[string]ReplayRecord
	fail    error
	failUpdate bool
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:     make(map[ID]Verification),
		subjects: make(map[string]SubjectBinding),
		byVer:    make(map[ID]string),
		replays:  make(map[string]ReplayRecord),
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

func (m *MemoryStore) SetFailUpdate(fail bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failUpdate = fail
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
	if m.failUpdate {
		return errUnavailable
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

func (m *MemoryStore) BindSubject(ctx context.Context, b SubjectBinding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if validateSubjectBinding(b) != nil {
		return errInvalidVerification
	}
	if existing, ok := m.subjects[b.SubjectRef]; ok {
		if existing.VerificationID == b.VerificationID {
			return nil
		}
		return errConflict
	}
	if ref, ok := m.byVer[b.VerificationID]; ok && ref != b.SubjectRef {
		return errConflict
	}
	m.subjects[b.SubjectRef] = b
	m.byVer[b.VerificationID] = b.SubjectRef
	return nil
}

func (m *MemoryStore) LookupSubject(ctx context.Context, subjectRef string) (SubjectBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return SubjectBinding{}, err
	}
	b, ok := m.subjects[subjectRef]
	if !ok {
		return SubjectBinding{}, errNotFound
	}
	return b, nil
}

func (m *MemoryStore) LookupSubjectByVerification(ctx context.Context, verificationID ID) (SubjectBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return SubjectBinding{}, err
	}
	ref, ok := m.byVer[verificationID]
	if !ok {
		return SubjectBinding{}, errNotFound
	}
	return m.subjects[ref], nil
}

func (m *MemoryStore) GetReplay(ctx context.Context, decisionID string) (ReplayRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return ReplayRecord{}, err
	}
	rec, ok := m.replays[decisionID]
	if !ok {
		return ReplayRecord{}, errNotFound
	}
	return rec, nil
}

func (m *MemoryStore) LatestReplayIssuedAt(ctx context.Context, subjectRef string) (time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return time.Time{}, false, err
	}
	var latest time.Time
	found := false
	for _, rec := range m.replays {
		if rec.SubjectRef != subjectRef {
			continue
		}
		if !found || rec.IssuedAt.After(latest) {
			latest = rec.IssuedAt
			found = true
		}
	}
	return latest, found, nil
}

func (m *MemoryStore) InsertReplay(ctx context.Context, rec ReplayRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if _, ok := m.replays[rec.DecisionID]; ok {
		return errConflict
	}
	m.replays[rec.DecisionID] = rec
	return nil
}

func (m *MemoryStore) ApplyReplayAndUpdate(ctx context.Context, rec ReplayRecord, next Verification, expectedUpdatedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockedErr(ctx); err != nil {
		return err
	}
	if m.failUpdate {
		return errUnavailable
	}
	if _, ok := m.replays[rec.DecisionID]; ok {
		return errConflict
	}
	if err := next.Validate(); err != nil {
		return err
	}
	cur, ok := m.byID[next.ID]
	if !ok {
		return errNotFound
	}
	if !cur.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	m.replays[rec.DecisionID] = rec
	m.byID[next.ID] = cloneVerification(next)
	return nil
}

func (m *MemoryStore) ReplayCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.replays)
}

func validateSubjectBinding(b SubjectBinding) error {
	if b.SubjectRef == "" || b.VerificationID.IsZero() || b.ListingID.IsZero() || !b.VerificationType.valid() || b.CreatedAt.IsZero() {
		return errInvalidVerification
	}
	return nil
}
