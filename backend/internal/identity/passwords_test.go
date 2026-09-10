package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func testPasswordPolicy() PasswordPolicy {
	return PasswordPolicy{
		MemoryKiB:   8,
		Iterations:  1,
		Parallelism: 1,
		SaltLen:     16,
		KeyLen:      16,
	}
}

func TestInvalidPasswordPolicy(t *testing.T) {
	store := newMemPasswordStore()
	now := func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }
	cases := []PasswordPolicy{
		{MemoryKiB: 0, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 16},
		{MemoryKiB: 8, Iterations: 0, Parallelism: 1, SaltLen: 16, KeyLen: 16},
		{MemoryKiB: 8, Iterations: 1, Parallelism: 0, SaltLen: 16, KeyLen: 16},
		{MemoryKiB: 8, Iterations: 1, Parallelism: 1, SaltLen: 8, KeyLen: 16},
		{MemoryKiB: 8, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 8},
	}
	for _, p := range cases {
		if _, err := NewPasswords(store, p, now); !errors.Is(err, errInvalidPasswordPolicy) {
			t.Fatalf("policy %+v: err = %v, want %v", p, err, errInvalidPasswordPolicy)
		}
	}
}

func TestPasswordHashAndVerify(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestPasswords(t)
	user := store.putUser(t, User{})
	password := []byte("correct horse battery staple")

	if err := svc.Set(ctx, user.ID, password); err != nil {
		t.Fatalf("Set: %v", err)
	}
	stored := store.must(t, user.ID)
	if stored.PasswordHash == string(password) || strings.Contains(stored.PasswordHash, string(password)) {
		t.Fatal("plaintext must not be persisted")
	}
	if !strings.HasPrefix(stored.PasswordHash, "$argon2id$v=19$") {
		t.Fatalf("stored hash is not PHC Argon2id: %s", stored.PasswordHash)
	}
	if stored.DisabledAt != nil {
		t.Fatal("new credential must not be disabled")
	}

	got, err := svc.Verify(ctx, user.ID, password)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.NeedsRehash {
		t.Fatal("matching policy must not need rehash")
	}
}

func TestPasswordWrongPassword(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestPasswords(t)
	user := store.putUser(t, User{})
	if err := svc.Set(ctx, user.ID, []byte("right-password")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, user.ID, []byte("wrong-password")); !errors.Is(err, errUnauthenticated) {
		t.Fatalf("err = %v, want %v", err, errUnauthenticated)
	}
}

func TestPasswordMalformedPHCFailsClosed(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestPasswords(t)
	user := store.putUser(t, User{})
	if err := svc.Set(ctx, user.ID, []byte("ok")); err != nil {
		t.Fatal(err)
	}
	cred := store.must(t, user.ID)
	cred.PasswordHash = "not-a-phc"
	store.creds[user.ID] = cred

	if _, err := svc.Verify(ctx, user.ID, []byte("ok")); !errors.Is(err, errMalformedPasswordHash) {
		t.Fatalf("err = %v, want %v", err, errMalformedPasswordHash)
	}
	if errors.Is(errMalformedPasswordHash, errUnavailable) {
		t.Fatal("malformed PHC must fail closed, not as unavailability")
	}
}

func TestPasswordSaltUniqueness(t *testing.T) {
	policy := testPasswordPolicy()
	password := []byte("same-password")
	a, err := hashPassword(password, policy)
	if err != nil {
		t.Fatal(err)
	}
	b, err := hashPassword(password, policy)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("identical PHC encodings imply reused salt")
	}
	da, err := decodePHC(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := decodePHC(b)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(da.salt, db.salt) {
		t.Fatal("salts must be unique per hash")
	}
}

func TestPasswordDisabledCredential(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestPasswords(t)
	user := store.putUser(t, User{})
	password := []byte("fallback-secret")
	if err := svc.Set(ctx, user.ID, password); err != nil {
		t.Fatal(err)
	}
	if err := svc.Disable(ctx, user.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if store.must(t, user.ID).DisabledAt == nil {
		t.Fatal("disabled_at must be set")
	}
	if _, err := svc.Verify(ctx, user.ID, password); !errors.Is(err, errUnauthenticated) {
		t.Fatalf("disabled verify err = %v, want %v", err, errUnauthenticated)
	}
}

func TestPasswordDisabledOrDeletedUser(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestPasswords(t)
	password := []byte("fallback-secret")

	disabledAt := now()
	disabled := store.putUser(t, User{DisabledAt: &disabledAt})
	if err := svc.Set(ctx, disabled.ID, password); !errors.Is(err, errAccountIneligible) {
		t.Fatalf("set disabled err = %v, want %v", err, errAccountIneligible)
	}

	deletedAt := now()
	deleted := store.putUser(t, User{DeletedAt: &deletedAt})
	if err := svc.Set(ctx, deleted.ID, password); !errors.Is(err, errAccountIneligible) {
		t.Fatalf("set deleted err = %v, want %v", err, errAccountIneligible)
	}

	active := store.putUser(t, User{})
	if err := svc.Set(ctx, active.ID, password); err != nil {
		t.Fatal(err)
	}
	active.DisabledAt = &disabledAt
	store.users[active.ID] = active
	if _, err := svc.Verify(ctx, active.ID, password); !errors.Is(err, errAccountIneligible) {
		t.Fatalf("verify disabled user err = %v, want %v", err, errAccountIneligible)
	}

	active.DisabledAt = nil
	active.DeletedAt = &deletedAt
	store.users[active.ID] = active
	if _, err := svc.Verify(ctx, active.ID, password); !errors.Is(err, errAccountIneligible) {
		t.Fatalf("verify deleted user err = %v, want %v", err, errAccountIneligible)
	}
}

func TestPasswordNeedsRehash(t *testing.T) {
	ctx := context.Background()
	store := newMemPasswordStore()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	oldPolicy := testPasswordPolicy()
	svc, err := NewPasswords(store, oldPolicy, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	user := store.putUser(t, User{})
	password := []byte("rehash-me")
	if err := svc.Set(ctx, user.ID, password); err != nil {
		t.Fatal(err)
	}

	upgraded := oldPolicy
	upgraded.MemoryKiB = 16
	upgraded.Iterations = 2
	next, err := NewPasswords(store, upgraded, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	got, err := next.Verify(ctx, user.ID, password)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !got.NeedsRehash {
		t.Fatal("parameter upgrade must need rehash")
	}
}

func TestPasswordStoreErrorsAreNotWrongPassword(t *testing.T) {
	ctx := context.Background()
	store := newMemPasswordStore()
	store.unavailable = true
	svc, err := NewPasswords(store, testPasswordPolicy(), nil)
	if err != nil {
		t.Fatal(err)
	}
	id := mustID(t)
	if err := svc.Set(ctx, id, []byte("x")); !errors.Is(err, errUnavailable) {
		t.Fatalf("set err = %v, want %v", err, errUnavailable)
	}
	if _, err := svc.Verify(ctx, id, []byte("x")); !errors.Is(err, errUnavailable) {
		t.Fatalf("verify err = %v, want %v", err, errUnavailable)
	}
	if errors.Is(errUnavailable, errUnauthenticated) {
		t.Fatal("store unavailability must not be an authentication failure")
	}
}

func TestDummyVerifyDoesNotPersist(t *testing.T) {
	svc, store, _ := newTestPasswords(t)
	before := len(store.creds)
	svc.DummyVerify([]byte("enumeration-probe"))
	if len(store.creds) != before {
		t.Fatal("dummy PHC must never be persisted")
	}
	if svc.dummyPHC == "" {
		t.Fatal("process-local dummy PHC must exist")
	}
	if _, err := decodePHC(svc.dummyPHC); err != nil {
		t.Fatalf("dummy PHC: %v", err)
	}
}

func newTestPasswords(t *testing.T) (*Passwords, *memPasswordStore, func() time.Time) {
	t.Helper()
	store := newMemPasswordStore()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	svc, err := NewPasswords(store, testPasswordPolicy(), clock)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, clock
}

type memPasswordStore struct {
	users       map[ID]User
	creds       map[ID]PasswordCredential
	unavailable bool
}

func newMemPasswordStore() *memPasswordStore {
	return &memPasswordStore{
		users: make(map[ID]User),
		creds: make(map[ID]PasswordCredential),
	}
}

func (m *memPasswordStore) putUser(t *testing.T, u User) User {
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

func (m *memPasswordStore) must(t *testing.T, userID ID) PasswordCredential {
	t.Helper()
	c, ok := m.creds[userID]
	if !ok {
		t.Fatalf("password credential for %v missing", userID)
	}
	return c
}

func (m *memPasswordStore) check() error {
	if m.unavailable {
		return errUnavailable
	}
	return nil
}

func (m *memPasswordStore) GetUser(ctx context.Context, id ID) (User, error) {
	if err := m.check(); err != nil {
		return User{}, err
	}
	u, ok := m.users[id]
	if !ok {
		return User{}, errNotFound
	}
	return u, nil
}

func (m *memPasswordStore) GetPasswordCredential(ctx context.Context, userID ID) (PasswordCredential, error) {
	if err := m.check(); err != nil {
		return PasswordCredential{}, err
	}
	c, ok := m.creds[userID]
	if !ok {
		return PasswordCredential{}, errNotFound
	}
	return clonePassword(c), nil
}

func (m *memPasswordStore) UpsertPasswordCredential(ctx context.Context, credential PasswordCredential) error {
	if err := m.check(); err != nil {
		return err
	}
	m.creds[credential.UserID] = clonePassword(credential)
	return nil
}

func (m *memPasswordStore) DisablePasswordCredential(ctx context.Context, userID ID, at time.Time) error {
	if err := m.check(); err != nil {
		return err
	}
	c, ok := m.creds[userID]
	if !ok {
		return errNotFound
	}
	disabled := at
	c.DisabledAt = &disabled
	c.UpdatedAt = at
	m.creds[userID] = c
	return nil
}

func clonePassword(c PasswordCredential) PasswordCredential {
	if c.DisabledAt != nil {
		t := *c.DisabledAt
		c.DisabledAt = &t
	}
	return c
}
