package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestInvalidSessionPolicy(t *testing.T) {
	store := newMemStore()
	now := func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }
	cases := []SessionPolicy{
		{Idle: 0, Absolute: time.Hour},
		{Idle: time.Hour, Absolute: 0},
		{Idle: -time.Second, Absolute: time.Hour},
		{Idle: 2 * time.Hour, Absolute: time.Hour},
	}
	for _, p := range cases {
		if _, err := NewSessions(store, p, nil, SessionCachePolicy{}, now); !errors.Is(err, errInvalidPolicy) {
			t.Fatalf("policy %+v: err = %v, want %v", p, err, errInvalidPolicy)
		}
	}
}

func TestCreateAndResolveValidSession(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestSessions(t)
	user := store.putUser(t, User{})

	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if issued.RawToken == "" {
		t.Fatal("raw token must be returned once")
	}
	if len(issued.Session.TokenHash) != TokenHashSize {
		t.Fatal("session must store token hash only")
	}
	if bytes.Contains(issued.Session.TokenHash, []byte(issued.RawToken)) {
		t.Fatal("raw token must not appear in stored hash")
	}
	stored := store.mustSession(t, issued.Session.ID)
	if !bytes.Equal(stored.TokenHash, issued.Session.TokenHash) {
		t.Fatal("store must persist hash")
	}
	if store.rawTokens[issued.Session.ID] != "" {
		t.Fatal("store must never persist raw token")
	}

	got, err := svc.Resolve(ctx, issued.RawToken)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID != issued.Session.ID || got.UserID != user.ID {
		t.Fatalf("resolved session mismatch: %+v", got)
	}
	if got.IdleExpiresAt != now().Add(time.Hour) {
		t.Fatalf("idle expiry = %v", got.IdleExpiresAt)
	}
	if got.AbsoluteExpiresAt != now().Add(24*time.Hour) {
		t.Fatalf("absolute expiry = %v", got.AbsoluteExpiresAt)
	}
}

func TestResolveRevokedSession(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestSessions(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, issued.Session.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errSessionRevoked {
		t.Fatalf("Resolve err = %v, want %v", err, errSessionRevoked)
	}
}

func TestResolveIdleExpiredSession(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{at: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)}
	store := newMemStore()
	svc := mustSessions(t, store, clock.now)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(time.Hour)
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errSessionIdleExpired {
		t.Fatalf("Resolve err = %v, want %v", err, errSessionIdleExpired)
	}
}

func TestResolveAbsoluteExpiredSession(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{at: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)}
	store := newMemStore()
	svc := mustSessions(t, store, clock.now)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	row := store.mustSession(t, issued.Session.ID)
	row.IdleExpiresAt = clock.at.Add(time.Hour)
	row.AbsoluteExpiresAt = clock.at
	store.sessions[row.ID] = row
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errSessionAbsExpired {
		t.Fatalf("Resolve err = %v, want %v", err, errSessionAbsExpired)
	}
}

func TestResolveDisabledAndDeletedUser(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestSessions(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	disabled := now()
	u := store.users[user.ID]
	u.DisabledAt = &disabled
	store.users[user.ID] = u
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errAccountIneligible {
		t.Fatalf("disabled err = %v, want %v", err, errAccountIneligible)
	}

	u.DisabledAt = nil
	deleted := now()
	u.DeletedAt = &deleted
	store.users[user.ID] = u
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errAccountIneligible {
		t.Fatalf("deleted err = %v, want %v", err, errAccountIneligible)
	}
}

func TestCreateRejectsDisabledUser(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestSessions(t)
	disabled := now()
	user := store.putUser(t, User{DisabledAt: &disabled})
	if _, err := svc.Create(ctx, user.ID, nil); err != errAccountIneligible {
		t.Fatalf("Create err = %v, want %v", err, errAccountIneligible)
	}
}

func TestResolveInvalidTokenIsUnauthenticated(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestSessions(t)
	if _, err := svc.Resolve(ctx, "not-a-token"); err != errUnauthenticated {
		t.Fatalf("malformed err = %v, want %v", err, errUnauthenticated)
	}
	if store.hashLookups != 0 {
		t.Fatal("malformed token must not hit the store")
	}

	if _, err := svc.Resolve(ctx, validLookingToken(t)); err != errUnauthenticated {
		t.Fatalf("unknown err = %v, want %v", err, errUnauthenticated)
	}
}

func TestStoreFailureIsNotAuthDecision(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestSessions(t)
	store.unavailable = true
	if _, err := svc.Resolve(ctx, validLookingToken(t)); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want %v", err, errUnavailable)
	}
	if errors.Is(errUnavailable, errUnauthenticated) {
		t.Fatal("unavailable must not be unauthenticated")
	}
}

func TestTouchExtendsIdleNotAbsolute(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{at: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)}
	store := newMemStore()
	svc := mustSessions(t, store, clock.now)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	absolute := issued.Session.AbsoluteExpiresAt
	clock.at = clock.at.Add(10 * time.Minute)
	if err := svc.Touch(ctx, issued.Session.ID); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got := store.mustSession(t, issued.Session.ID)
	if got.LastSeenAt != clock.at {
		t.Fatalf("last_seen_at = %v, want %v", got.LastSeenAt, clock.at)
	}
	if got.IdleExpiresAt != clock.at.Add(time.Hour) {
		t.Fatalf("idle_expires_at = %v", got.IdleExpiresAt)
	}
	if got.AbsoluteExpiresAt != absolute {
		t.Fatal("absolute expiry must not move")
	}
}

func TestRevokeOneDoesNotRevokeOthers(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestSessions(t)
	user := store.putUser(t, User{})
	a, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, a.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, a.RawToken); err != errSessionRevoked {
		t.Fatalf("revoked session: %v", err)
	}
	if _, err := svc.Resolve(ctx, b.RawToken); err != nil {
		t.Fatalf("other session: %v", err)
	}
}

func TestRevokeAllForUser(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestSessions(t)
	user := store.putUser(t, User{})
	other := store.putUser(t, User{})
	a, _ := svc.Create(ctx, user.ID, nil)
	b, _ := svc.Create(ctx, user.ID, nil)
	c, _ := svc.Create(ctx, other.ID, nil)
	if err := svc.RevokeAllForUser(ctx, user.ID); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}
	if _, err := svc.Resolve(ctx, a.RawToken); err != errSessionRevoked {
		t.Fatalf("a: %v", err)
	}
	if _, err := svc.Resolve(ctx, b.RawToken); err != errSessionRevoked {
		t.Fatalf("b: %v", err)
	}
	if _, err := svc.Resolve(ctx, c.RawToken); err != nil {
		t.Fatalf("other user: %v", err)
	}
}

func TestRevokeAllBumpsDurableEpochAtomically(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestSessions(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if store.users[user.ID].SessionEpoch != 0 {
		t.Fatalf("session_epoch = %d, want 0", store.users[user.ID].SessionEpoch)
	}
	if err := svc.RevokeAllForUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if store.users[user.ID].SessionEpoch != 1 {
		t.Fatalf("session_epoch = %d, want 1", store.users[user.ID].SessionEpoch)
	}
	if store.mustSession(t, issued.Session.ID).RevokedAt == nil {
		t.Fatal("revoke-all must revoke durable sessions with the epoch bump")
	}
	store.unavailable = true
	if err := svc.RevokeAllForUser(ctx, user.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want %v", err, errUnavailable)
	}
	if store.users[user.ID].SessionEpoch != 1 {
		t.Fatal("failed revoke-all must not change session_epoch")
	}
}

func TestErrorsDoNotContainRawToken(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestSessions(t)
	raw := validLookingToken(t)
	_, err := svc.Resolve(ctx, raw)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), raw) {
		t.Fatal("error must not include raw token")
	}
}

type testClock struct {
	at time.Time
}

func (c *testClock) now() time.Time { return c.at }

func newTestSessions(t *testing.T) (*Sessions, *memStore, func() time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store := newMemStore()
	return mustSessions(t, store, clock), store, clock
}

func mustSessions(t *testing.T, store sessionStore, now func() time.Time) *Sessions {
	t.Helper()
	svc, err := NewSessions(store, SessionPolicy{Idle: time.Hour, Absolute: 24 * time.Hour}, nil, SessionCachePolicy{}, now)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

type memStore struct {
	users       map[ID]User
	devices     map[ID]Device
	sessions    map[ID]Session
	byHash      map[string]ID
	rawTokens   map[ID]string
	creds       map[ID]PasswordCredential
	resetProofs map[ID]PasswordResetProof
	hashLookups int
	unavailable bool
	failUpsert  bool
}

func newMemStore() *memStore {
	return &memStore{
		users:       make(map[ID]User),
		devices:     make(map[ID]Device),
		sessions:    make(map[ID]Session),
		byHash:      make(map[string]ID),
		rawTokens:   make(map[ID]string),
		creds:       make(map[ID]PasswordCredential),
		resetProofs: make(map[ID]PasswordResetProof),
	}
}

func (m *memStore) putUser(t *testing.T, u User) User {
	t.Helper()
	if u.ID.IsZero() {
		u.ID = mustID(t)
	}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
		u.UpdatedAt = now
	}
	m.users[u.ID] = u
	return u
}

func (m *memStore) mustSession(t *testing.T, id ID) Session {
	t.Helper()
	s, ok := m.sessions[id]
	if !ok {
		t.Fatalf("session %v missing", id)
	}
	return s
}

func (m *memStore) check() error {
	if m.unavailable {
		return errUnavailable
	}
	return nil
}

func (m *memStore) GetUser(ctx context.Context, id ID) (User, error) {
	if err := m.check(); err != nil {
		return User{}, err
	}
	u, ok := m.users[id]
	if !ok {
		return User{}, errNotFound
	}
	return u, nil
}

func (m *memStore) GetDevice(ctx context.Context, id ID) (Device, error) {
	if err := m.check(); err != nil {
		return Device{}, err
	}
	d, ok := m.devices[id]
	if !ok {
		return Device{}, errNotFound
	}
	return d, nil
}

func (m *memStore) InsertSession(ctx context.Context, session Session) error {
	if err := m.check(); err != nil {
		return err
	}
	cp := session
	cp.TokenHash = bytes.Clone(session.TokenHash)
	m.sessions[session.ID] = cp
	m.byHash[string(session.TokenHash)] = session.ID
	return nil
}

func (m *memStore) GetSession(ctx context.Context, id ID) (Session, error) {
	if err := m.check(); err != nil {
		return Session{}, err
	}
	s, ok := m.sessions[id]
	if !ok {
		return Session{}, errNotFound
	}
	return s, nil
}

func (m *memStore) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error) {
	m.hashLookups++
	if err := m.check(); err != nil {
		return Session{}, err
	}
	id, ok := m.byHash[string(tokenHash)]
	if !ok {
		return Session{}, errNotFound
	}
	return m.sessions[id], nil
}

func (m *memStore) UpdateSessionActivity(ctx context.Context, id ID, lastSeenAt, idleExpiresAt time.Time) error {
	if err := m.check(); err != nil {
		return err
	}
	s, ok := m.sessions[id]
	if !ok {
		return errNotFound
	}
	if s.RevokedAt != nil {
		return errSessionRevoked
	}
	if !lastSeenAt.Before(s.IdleExpiresAt) || !lastSeenAt.Before(s.AbsoluteExpiresAt) {
		return errUnauthenticated
	}
	s.LastSeenAt = lastSeenAt
	s.IdleExpiresAt = idleExpiresAt
	m.sessions[id] = s
	return nil
}

func (m *memStore) RevokeSession(ctx context.Context, id ID, at time.Time) error {
	if err := m.check(); err != nil {
		return err
	}
	s, ok := m.sessions[id]
	if !ok {
		return errNotFound
	}
	if s.RevokedAt == nil {
		revoked := at
		s.RevokedAt = &revoked
		m.sessions[id] = s
	}
	return nil
}

func (m *memStore) RevokeSessionsForUser(ctx context.Context, userID ID, at time.Time) (int64, error) {
	if err := m.check(); err != nil {
		return 0, err
	}
	u, ok := m.users[userID]
	if !ok {
		return 0, errNotFound
	}
	for id, s := range m.sessions {
		if s.UserID != userID || s.RevokedAt != nil {
			continue
		}
		revoked := at
		s.RevokedAt = &revoked
		m.sessions[id] = s
	}
	u.SessionEpoch++
	u.UpdatedAt = at
	m.users[userID] = u
	return u.SessionEpoch, nil
}

func (m *memStore) ListSessionsForUser(ctx context.Context, userID ID) ([]Session, error) {
	if err := m.check(); err != nil {
		return nil, err
	}
	out := make([]Session, 0)
	for _, s := range m.sessions {
		if s.UserID == userID {
			cp := s
			cp.TokenHash = bytes.Clone(s.TokenHash)
			out = append(out, cp)
		}
	}
	return out, nil
}

func (m *memStore) RevokeOtherSessionsForUser(ctx context.Context, userID, keepSessionID ID, at time.Time) ([][]byte, error) {
	if err := m.check(); err != nil {
		return nil, err
	}
	hashes := make([][]byte, 0)
	for id, s := range m.sessions {
		if s.UserID != userID || s.ID == keepSessionID || s.RevokedAt != nil {
			continue
		}
		revoked := at
		s.RevokedAt = &revoked
		m.sessions[id] = s
		hashes = append(hashes, bytes.Clone(s.TokenHash))
	}
	return hashes, nil
}

func validLookingToken(t *testing.T) string {
	t.Helper()
	raw, _, err := GenerateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
