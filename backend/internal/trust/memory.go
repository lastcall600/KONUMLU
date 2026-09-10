package trust

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is an in-process derived projection for tests.
type MemoryStore struct {
	mu        sync.Mutex
	processed map[ID]struct{}
	profiles  map[ID]UserProfile
	history   map[historyKey]HistoryEntry
	fail      error
}

type historyKey struct {
	interaction ID
	user        ID
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		processed: make(map[ID]struct{}),
		profiles:  make(map[ID]UserProfile),
		history:   make(map[historyKey]HistoryEntry),
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

func (m *MemoryStore) ApplyCompleted(ctx context.Context, event CompletedInteraction, policy LevelPolicy, now time.Time) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.processed[event.EventID]; ok {
		return nil
	}
	m.processed[event.EventID] = struct{}{}
	req := HistoryEntry{
		InteractionID:      event.InteractionID,
		UserID:             event.RequesterUserID,
		Role:               RoleRequester,
		ListingID:          event.ListingID,
		InteractionType:    event.InteractionType,
		VerificationMethod: event.VerificationMethod,
		VerifiedAt:         event.VerifiedAt.UTC(),
	}
	prov := req
	prov.UserID = event.ProviderUserID
	prov.Role = RoleProvider
	if err := m.insertHistoryLocked(req, policy, now); err != nil {
		return err
	}
	if err := m.insertHistoryLocked(prov, policy, now); err != nil {
		return err
	}
	return nil
}

func (m *MemoryStore) ApplyVerifiedReview(ctx context.Context, event VerifiedReview, now time.Time) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.processed[event.EventID]; ok {
		return nil
	}
	m.processed[event.EventID] = struct{}{}
	if err := m.bumpReviewerLocked(event.ReviewerUserID, event.CreatedAt, now); err != nil {
		return err
	}
	return m.bumpProviderServiceLocked(event.ProviderUserID, event.ProviderService, event.CreatedAt, now)
}

func (m *MemoryStore) insertHistoryLocked(row HistoryEntry, policy LevelPolicy, now time.Time) error {
	if err := row.Validate(); err != nil {
		return err
	}
	key := historyKey{interaction: row.InteractionID, user: row.UserID}
	if _, exists := m.history[key]; exists {
		return nil
	}
	m.history[key] = row
	if !countsTowardTrustLevel(row.InteractionType) {
		return nil
	}
	return m.bumpLocked(row.UserID, row.Role, row.VerifiedAt, policy, now)
}

func (m *MemoryStore) bumpLocked(userID ID, role string, verifiedAt time.Time, policy LevelPolicy, now time.Time) error {
	p, ok := m.profiles[userID]
	if !ok {
		p = ZeroProfile(userID, now)
	}
	p.VerifiedInteractionCount++
	switch role {
	case RoleRequester:
		p.RequesterVerifiedInteractionCount++
	case RoleProvider:
		p.ProviderVerifiedInteractionCount++
	default:
		return errInvalidEvent
	}
	if p.LastVerifiedInteractionAt == nil || verifiedAt.After(*p.LastVerifiedInteractionAt) {
		at := verifiedAt.UTC()
		p.LastVerifiedInteractionAt = &at
	}
	p.TrustLevel = policy.Level(p.VerifiedInteractionCount)
	p.UpdatedAt = now.UTC()
	if err := p.Validate(); err != nil {
		return err
	}
	m.profiles[userID] = p
	return nil
}

func (m *MemoryStore) bumpReviewerLocked(userID ID, createdAt, now time.Time) error {
	p, ok := m.profiles[userID]
	if !ok {
		p = ZeroProfile(userID, now)
	}
	p.VerifiedReviewCount++
	if p.LastVerifiedReviewAt == nil || createdAt.After(*p.LastVerifiedReviewAt) {
		at := createdAt.UTC()
		p.LastVerifiedReviewAt = &at
	}
	p.UpdatedAt = now.UTC()
	if err := p.Validate(); err != nil {
		return err
	}
	m.profiles[userID] = p
	return nil
}

func (m *MemoryStore) bumpProviderServiceLocked(userID ID, rating int, createdAt, now time.Time) error {
	p, ok := m.profiles[userID]
	if !ok {
		p = ZeroProfile(userID, now)
	}
	p.ProviderServiceReviewCount++
	p.ProviderServiceRatingSum += rating
	if p.LastProviderServiceReviewAt == nil || createdAt.After(*p.LastProviderServiceReviewAt) {
		at := createdAt.UTC()
		p.LastProviderServiceReviewAt = &at
	}
	p.UpdatedAt = now.UTC()
	if err := p.Validate(); err != nil {
		return err
	}
	m.profiles[userID] = p
	return nil
}

func (m *MemoryStore) GetProfile(ctx context.Context, userID ID) (UserProfile, error) {
	if m == nil {
		return UserProfile{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return UserProfile{}, err
	}
	if userID.IsZero() {
		return UserProfile{}, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return UserProfile{}, m.fail
	}
	p, ok := m.profiles[userID]
	if !ok {
		return UserProfile{}, errNotFound
	}
	return cloneProfile(p), nil
}

func (m *MemoryStore) ListHistory(ctx context.Context, userID ID) ([]HistoryEntry, error) {
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
	out := make([]HistoryEntry, 0)
	for _, row := range m.history {
		if row.UserID == userID {
			out = append(out, row)
		}
	}
	return out, nil
}

func cloneProfile(p UserProfile) UserProfile {
	if p.LastVerifiedInteractionAt != nil {
		t := p.LastVerifiedInteractionAt.UTC()
		p.LastVerifiedInteractionAt = &t
	}
	if p.LastVerifiedReviewAt != nil {
		t := p.LastVerifiedReviewAt.UTC()
		p.LastVerifiedReviewAt = &t
	}
	if p.LastProviderServiceReviewAt != nil {
		t := p.LastProviderServiceReviewAt.UTC()
		p.LastProviderServiceReviewAt = &t
	}
	return p
}

var _ projectionStore = (*MemoryStore)(nil)
