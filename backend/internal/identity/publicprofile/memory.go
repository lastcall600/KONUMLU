package publicprofile

import (
	"context"
	"sync"
	"time"

	"backend/internal/identity"
)

// MemoryStore is an in-process public-profile store for tests.
type MemoryStore struct {
	mu       sync.Mutex
	users    map[identity.ID]identity.User
	byUser   map[identity.ID]Profile
	byPublic map[identity.ID]identity.ID
	fail     error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:    make(map[identity.ID]identity.User),
		byUser:   make(map[identity.ID]Profile),
		byPublic: make(map[identity.ID]identity.ID),
	}
}

func (m *MemoryStore) PutUser(user identity.User) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user.ID] = user
}

func (m *MemoryStore) SetFail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) GetUser(ctx context.Context, id identity.ID) (identity.User, error) {
	if err := m.ready(ctx); err != nil {
		return identity.User{}, err
	}
	if id.IsZero() {
		return identity.User{}, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return identity.User{}, ErrNotFound
	}
	return u, nil
}

func (m *MemoryStore) GetByUserID(ctx context.Context, userID identity.ID) (Profile, error) {
	if err := m.ready(ctx); err != nil {
		return Profile{}, err
	}
	if userID.IsZero() {
		return Profile{}, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.byUser[userID]
	if !ok {
		return Profile{}, ErrNotFound
	}
	return cloneProfile(p), nil
}

func (m *MemoryStore) GetByPublicID(ctx context.Context, publicID identity.ID) (storedPublic, error) {
	if err := m.ready(ctx); err != nil {
		return storedPublic{}, err
	}
	if publicID.IsZero() {
		return storedPublic{}, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	userID, ok := m.byPublic[publicID]
	if !ok {
		return storedPublic{}, ErrNotFound
	}
	p, ok := m.byUser[userID]
	if !ok {
		return storedPublic{}, ErrNotFound
	}
	u, ok := m.users[userID]
	if !ok {
		return storedPublic{}, ErrNotFound
	}
	return storedPublic{
		Profile:     cloneProfile(p),
		MemberSince: u.CreatedAt,
		DisabledAt:  u.DisabledAt,
		DeletedAt:   u.DeletedAt,
	}, nil
}

func (m *MemoryStore) Insert(ctx context.Context, profile Profile) error {
	if err := m.ready(ctx); err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byUser[profile.UserID]; ok {
		return nil
	}
	if existingUser, ok := m.byPublic[profile.PublicProfileID]; ok && existingUser != profile.UserID {
		return ErrUnavailable
	}
	m.byUser[profile.UserID] = cloneProfile(profile)
	m.byPublic[profile.PublicProfileID] = profile.UserID
	return nil
}

func (m *MemoryStore) UpdateDisplayName(ctx context.Context, userID identity.ID, displayName *string, updatedAt time.Time) error {
	if err := m.ready(ctx); err != nil {
		return err
	}
	if userID.IsZero() || updatedAt.IsZero() {
		return ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.byUser[userID]
	if !ok {
		return ErrNotFound
	}
	p.DisplayName = cloneDisplay(displayName)
	p.UpdatedAt = updatedAt
	m.byUser[userID] = p
	return nil
}

func (m *MemoryStore) UpdateModeration(ctx context.Context, next Profile, expectedUpdatedAt time.Time) error {
	if err := m.ready(ctx); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if expectedUpdatedAt.IsZero() {
		return ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	userID, ok := m.byPublic[next.PublicProfileID]
	if !ok {
		return ErrNotFound
	}
	current, ok := m.byUser[userID]
	if !ok {
		return ErrNotFound
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	m.byUser[userID] = cloneProfile(next)
	return nil
}

func (m *MemoryStore) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	return nil
}

func cloneProfile(p Profile) Profile {
	p.DisplayName = cloneDisplay(p.DisplayName)
	return p
}

func cloneDisplay(in *string) *string {
	if in == nil {
		return nil
	}
	s := *in
	return &s
}
