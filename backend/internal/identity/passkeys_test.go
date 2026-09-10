package identity

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestPasskeyActiveRequiresUnrevokedValidMaterial(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	c := validPasskey(t, now)
	if !c.Active() {
		t.Fatal("valid credential should be active")
	}
	revoked := now
	c.RevokedAt = &revoked
	if c.Active() {
		t.Fatal("revoked credential must not authenticate")
	}
	c.RevokedAt = nil
	c.CredentialID = nil
	if c.Active() {
		t.Fatal("empty credential_id must not be active")
	}
}

func TestPasskeyValidate(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	c := validPasskey(t, now)
	if err := c.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	c.SignCount = -1
	if err := c.Validate(); err != errInvalidPasskey {
		t.Fatalf("negative sign_count err = %v, want %v", err, errInvalidPasskey)
	}
	c = validPasskey(t, now)
	c.PublicKey = nil
	if err := c.Validate(); err != errInvalidPasskey {
		t.Fatalf("empty public key err = %v, want %v", err, errInvalidPasskey)
	}
	c = validPasskey(t, now)
	c.ID = ID{}
	if err := c.Validate(); err != errZeroID {
		t.Fatalf("zero id err = %v, want %v", err, errZeroID)
	}
}

func TestApplySuccessfulUseSignCount(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	used := now.Add(time.Minute)

	c := validPasskey(t, now)
	c.SignCount = 0
	next, err := c.ApplySuccessfulUse(0, true, used)
	if err != nil {
		t.Fatalf("zero to zero: %v", err)
	}
	if next.SignCount != 0 || !next.BackupState || next.LastUsedAt == nil || !next.LastUsedAt.Equal(used) {
		t.Fatalf("zero-counter use mismatch: %+v", next)
	}

	next, err = c.ApplySuccessfulUse(7, false, used)
	if err != nil {
		t.Fatalf("zero to positive: %v", err)
	}
	if next.SignCount != 7 {
		t.Fatalf("sign_count = %d, want 7", next.SignCount)
	}

	c.SignCount = 10
	next, err = c.ApplySuccessfulUse(10, false, used)
	if err != nil {
		t.Fatalf("equal non-zero: %v", err)
	}
	if next.SignCount != 10 {
		t.Fatalf("sign_count = %d, want 10", next.SignCount)
	}

	next, err = c.ApplySuccessfulUse(11, true, used)
	if err != nil {
		t.Fatalf("increase: %v", err)
	}
	if next.SignCount != 11 || !next.BackupState {
		t.Fatalf("increased mismatch: %+v", next)
	}

	_, err = c.ApplySuccessfulUse(9, false, used)
	if err != errSignCountNotMonotonic {
		t.Fatalf("decrease err = %v, want %v", err, errSignCountNotMonotonic)
	}
	if c.SignCount != 10 {
		t.Fatal("receiver must not change on failed apply")
	}

	revoked := now
	c.RevokedAt = &revoked
	if _, err := c.ApplySuccessfulUse(12, false, used); err != errCredentialRevoked {
		t.Fatalf("revoked err = %v, want %v", err, errCredentialRevoked)
	}
}

func TestPasskeyCreateFindListRevokeAndUse(t *testing.T) {
	ctx := context.Background()
	store := newMemPasskeyStore()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	svc, err := NewPasskeys(store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	user := mustID(t)
	a := validPasskey(t, now)
	a.UserID = user
	a.CredentialID = []byte{0x01, 0x02}
	a.PublicKey = []byte{0xaa, 0xbb, 0xcc}
	b := validPasskey(t, now)
	b.UserID = user
	b.CredentialID = []byte{0x03, 0x04}
	b.PublicKey = []byte{0xdd, 0xee}

	if err := svc.Create(ctx, a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := svc.Create(ctx, b); err != nil {
		t.Fatalf("create b: %v", err)
	}

	got, err := svc.FindActiveByCredentialID(ctx, a.CredentialID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.ID != a.ID || got.UserID != user || !bytes.Equal(got.PublicKey, a.PublicKey) {
		t.Fatalf("found mismatch: %+v", got)
	}
	if !bytes.Equal(got.CredentialID, []byte{0x01, 0x02}) {
		t.Fatal("credential_id must be stored as opaque bytes")
	}

	list, err := svc.ListActiveForUser(ctx, user)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}

	if err := svc.RecordSuccessfulUse(ctx, a.ID, 4, true); err != nil {
		t.Fatalf("use: %v", err)
	}
	stored := store.must(t, a.ID)
	if stored.SignCount != 4 || !stored.BackupState || stored.LastUsedAt == nil {
		t.Fatalf("successful-use not persisted: %+v", stored)
	}

	if err := svc.RecordSuccessfulUse(ctx, a.ID, 3, false); err != errSignCountNotMonotonic {
		t.Fatalf("decrease err = %v, want %v", err, errSignCountNotMonotonic)
	}
	if store.must(t, a.ID).SignCount != 4 {
		t.Fatal("stored non-zero sign_count must not decrease")
	}

	if err := svc.Revoke(ctx, a.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.FindActiveByCredentialID(ctx, a.CredentialID); !errors.Is(err, errNotFound) {
		t.Fatalf("revoked find err = %v, want %v", err, errNotFound)
	}
	list, err = svc.ListActiveForUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("active list after revoke = %+v", list)
	}
	if err := svc.RecordSuccessfulUse(ctx, a.ID, 5, true); err != errCredentialRevoked {
		t.Fatalf("revoked use err = %v, want %v", err, errCredentialRevoked)
	}
}

func TestPasskeyStoreErrorsAreNotAuthDecisions(t *testing.T) {
	ctx := context.Background()
	store := newMemPasskeyStore()
	store.unavailable = true
	svc, err := NewPasskeys(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := validPasskey(t, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	if err := svc.Create(ctx, c); !errors.Is(err, errUnavailable) {
		t.Fatalf("create err = %v, want %v", err, errUnavailable)
	}
	if _, err := svc.FindActiveByCredentialID(ctx, c.CredentialID); !errors.Is(err, errUnavailable) {
		t.Fatalf("find err = %v, want %v", err, errUnavailable)
	}
	if errors.Is(errUnavailable, errUnauthenticated) || errors.Is(errUnavailable, errCredentialRevoked) {
		t.Fatal("store unavailability must not be an authentication outcome")
	}
}

func validPasskey(t *testing.T, now time.Time) PasskeyCredential {
	t.Helper()
	return PasskeyCredential{
		ID:             mustID(t),
		UserID:         mustID(t),
		CredentialID:   []byte{0x10, 0x20, 0x30},
		PublicKey:      []byte{0x40, 0x50, 0x60, 0x70},
		SignCount:      0,
		BackupEligible: true,
		BackupState:    false,
		Transports:     []string{"internal"},
		CreatedAt:      now,
	}
}

type memPasskeyStore struct {
	creds       map[ID]PasskeyCredential
	unavailable bool
	insertFail  bool
	updateFail  bool
}

func newMemPasskeyStore() *memPasskeyStore {
	return &memPasskeyStore{creds: make(map[ID]PasskeyCredential)}
}

func (m *memPasskeyStore) must(t *testing.T, id ID) PasskeyCredential {
	t.Helper()
	c, ok := m.creds[id]
	if !ok {
		t.Fatalf("credential %v missing", id)
	}
	return c
}

func (m *memPasskeyStore) check() error {
	if m.unavailable {
		return errUnavailable
	}
	return nil
}

func (m *memPasskeyStore) clone(c PasskeyCredential) PasskeyCredential {
	c.CredentialID = cloneBytes(c.CredentialID)
	c.PublicKey = cloneBytes(c.PublicKey)
	c.Transports = cloneStrings(c.Transports)
	if c.LastUsedAt != nil {
		t := *c.LastUsedAt
		c.LastUsedAt = &t
	}
	if c.RevokedAt != nil {
		t := *c.RevokedAt
		c.RevokedAt = &t
	}
	return c
}

func (m *memPasskeyStore) InsertPasskey(ctx context.Context, credential PasskeyCredential) error {
	if err := m.check(); err != nil {
		return err
	}
	if m.insertFail {
		return errUnavailable
	}
	for _, existing := range m.creds {
		if bytes.Equal(existing.CredentialID, credential.CredentialID) {
			return errCredentialConflict
		}
	}
	m.creds[credential.ID] = m.clone(credential)
	return nil
}

func (m *memPasskeyStore) GetPasskey(ctx context.Context, id ID) (PasskeyCredential, error) {
	if err := m.check(); err != nil {
		return PasskeyCredential{}, err
	}
	c, ok := m.creds[id]
	if !ok {
		return PasskeyCredential{}, errNotFound
	}
	return m.clone(c), nil
}

func (m *memPasskeyStore) GetPasskeyByCredentialID(ctx context.Context, credentialID []byte) (PasskeyCredential, error) {
	if err := m.check(); err != nil {
		return PasskeyCredential{}, err
	}
	for _, c := range m.creds {
		if bytes.Equal(c.CredentialID, credentialID) {
			return m.clone(c), nil
		}
	}
	return PasskeyCredential{}, errNotFound
}

func (m *memPasskeyStore) GetActivePasskeyByCredentialID(ctx context.Context, credentialID []byte) (PasskeyCredential, error) {
	if err := m.check(); err != nil {
		return PasskeyCredential{}, err
	}
	for _, c := range m.creds {
		if bytes.Equal(c.CredentialID, credentialID) && c.RevokedAt == nil {
			return m.clone(c), nil
		}
	}
	return PasskeyCredential{}, errNotFound
}

func (m *memPasskeyStore) ListActivePasskeysForUser(ctx context.Context, userID ID) ([]PasskeyCredential, error) {
	if err := m.check(); err != nil {
		return nil, err
	}
	var out []PasskeyCredential
	for _, c := range m.creds {
		if c.UserID == userID && c.RevokedAt == nil {
			out = append(out, m.clone(c))
		}
	}
	return out, nil
}

func (m *memPasskeyStore) UpdatePasskeySuccessfulUse(ctx context.Context, id ID, signCount int64, backupState bool, lastUsedAt time.Time) error {
	if err := m.check(); err != nil {
		return err
	}
	if m.updateFail {
		return errUnavailable
	}
	c, ok := m.creds[id]
	if !ok || c.RevokedAt != nil {
		return errNotFound
	}
	if c.SignCount > 0 && signCount < c.SignCount {
		return errSignCountNotMonotonic
	}
	c.SignCount = signCount
	c.BackupState = backupState
	t := lastUsedAt
	c.LastUsedAt = &t
	m.creds[id] = c
	return nil
}

func (m *memPasskeyStore) RevokePasskey(ctx context.Context, id ID, at time.Time) error {
	if err := m.check(); err != nil {
		return err
	}
	c, ok := m.creds[id]
	if !ok {
		return errNotFound
	}
	if c.RevokedAt == nil {
		revoked := at
		c.RevokedAt = &revoked
		m.creds[id] = c
	}
	return nil
}
