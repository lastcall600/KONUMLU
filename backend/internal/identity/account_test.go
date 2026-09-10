package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCompleteSignupWithoutPassword(t *testing.T) {
	ctx := context.Background()
	svc, world, txns, now := newTestAccount(t)
	issued := world.mustIssue(t, IdentifierEmail, "owner@example.com", now)

	got, err := svc.CompleteSignup(ctx, CompleteSignupInput{SignupProof: issued.RawToken})
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID.IsZero() {
		t.Fatal("user id required")
	}
	if txns.last == nil || !txns.last.committed || txns.last.rolled {
		t.Fatal("account transaction must commit")
	}
	user := world.mustUser(t, got.UserID)
	if !user.EligibleForSession() {
		t.Fatalf("user ineligible: %+v", user)
	}
	ident := world.mustActiveIdent(t, IdentifierEmail, "owner@example.com")
	if ident.UserID != got.UserID || ident.VerifiedAt == nil || !ident.VerifiedActive() {
		t.Fatalf("verified identifier missing: %+v", ident)
	}
	if _, ok := world.creds[got.UserID]; ok {
		t.Fatal("password must be omitted")
	}
	world.assertProofConsumedOnce(t, issued.Proof.ID)
}

func TestCompleteSignupWithPassword(t *testing.T) {
	ctx := context.Background()
	svc, world, _, now := newTestAccount(t)
	issued := world.mustIssue(t, IdentifierPhone, "+15551234567", now)
	password := []byte("fallback-secret")

	got, err := svc.CompleteSignup(ctx, CompleteSignupInput{SignupProof: issued.RawToken, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	cred, ok := world.creds[got.UserID]
	if !ok || !cred.Active() {
		t.Fatal("password credential must be stored")
	}
	if !strings.HasPrefix(cred.PasswordHash, "$argon2id$") {
		t.Fatalf("hash scheme = %q", cred.PasswordHash)
	}
	if bytes.Contains([]byte(cred.PasswordHash), password) {
		t.Fatal("plaintext password must not be stored")
	}
	ident := world.mustActiveIdent(t, IdentifierPhone, "+15551234567")
	if ident.VerifiedAt == nil {
		t.Fatal("identifier must be verified")
	}
	world.assertProofConsumedOnce(t, issued.Proof.ID)
}

func TestCompleteSignupProofReplayRejected(t *testing.T) {
	ctx := context.Background()
	svc, world, _, now := newTestAccount(t)
	issued := world.mustIssue(t, IdentifierEmail, "once@example.com", now)
	if _, err := svc.CompleteSignup(ctx, CompleteSignupInput{SignupProof: issued.RawToken}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteSignup(ctx, CompleteSignupInput{SignupProof: issued.RawToken}); !errors.Is(err, errSignupProofConsumed) {
		t.Fatalf("replay err = %v, want consumed", err)
	}
	world.assertProofConsumedOnce(t, issued.Proof.ID)
	if world.userCount() != 1 {
		t.Fatalf("users = %d, want 1", world.userCount())
	}
}

func TestCompleteSignupExpiredProofRejected(t *testing.T) {
	ctx := context.Background()
	svc, world, txns, now := newTestAccount(t)
	issued := world.mustIssue(t, IdentifierEmail, "late@example.com", now)
	world.expireProof(issued.Proof.ID, now)

	if _, err := svc.CompleteSignup(ctx, CompleteSignupInput{SignupProof: issued.RawToken}); !errors.Is(err, errSignupProofExpired) {
		t.Fatalf("err = %v, want expired", err)
	}
	if txns.last == nil || txns.last.committed {
		t.Fatal("expired proof must not commit an account")
	}
	if world.userCount() != 0 {
		t.Fatal("expired proof must not create a user")
	}
	p := world.mustProof(t, issued.Proof.ID)
	if p.ConsumedAt != nil {
		t.Fatal("expired proof must remain unconsumed")
	}
}

func TestCompleteSignupDuplicateIdentifierRollsBack(t *testing.T) {
	ctx := context.Background()
	svc, world, txns, now := newTestAccount(t)
	owner := world.putUser(User{CreatedAt: now, UpdatedAt: now})
	world.putIdent(UserIdentifier{
		ID: mustID(t), UserID: owner.ID, Kind: IdentifierEmail, ValueCanonical: "taken@example.com",
		VerifiedAt: &now, CreatedAt: now,
	})
	issued := world.mustIssue(t, IdentifierEmail, "taken@example.com", now)

	if _, err := svc.CompleteSignup(ctx, CompleteSignupInput{SignupProof: issued.RawToken}); !errors.Is(err, errIdentifierConflict) {
		t.Fatalf("err = %v, want conflict", err)
	}
	if txns.last == nil || !txns.last.rolled || txns.last.committed {
		t.Fatal("duplicate identifier must roll back")
	}
	if world.userCount() != 1 {
		t.Fatalf("users = %d, want existing owner only", world.userCount())
	}
	p := world.mustProof(t, issued.Proof.ID)
	if p.ConsumedAt != nil {
		t.Fatal("duplicate identifier must not consume the proof")
	}
}

func TestCompleteSignupPasswordWriteFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	svc, world, txns, now := newTestAccount(t)
	world.failPassword = true
	issued := world.mustIssue(t, IdentifierEmail, "pwfail@example.com", now)

	if _, err := svc.CompleteSignup(ctx, CompleteSignupInput{
		SignupProof: issued.RawToken,
		Password:    []byte("fallback-secret"),
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want unavailable", err)
	}
	if world.userCount() != 0 || len(world.idents) != 0 || len(world.creds) != 0 {
		t.Fatal("password failure must leave no account rows")
	}
	if world.mustProof(t, issued.Proof.ID).ConsumedAt != nil {
		t.Fatal("password failure must not consume the proof")
	}
	if txns.last == nil || txns.last.committed {
		t.Fatal("password failure must not commit")
	}
}

func TestCompleteSignupDBFailureLeavesNoPartialAccount(t *testing.T) {
	ctx := context.Background()
	svc, world, txns, now := newTestAccount(t)
	world.failUser = true
	issued := world.mustIssue(t, IdentifierEmail, "dbfail@example.com", now)

	if _, err := svc.CompleteSignup(ctx, CompleteSignupInput{SignupProof: issued.RawToken}); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want unavailable", err)
	}
	if world.userCount() != 0 || len(world.idents) != 0 {
		t.Fatal("user insert failure must leave no partial account")
	}
	if world.mustProof(t, issued.Proof.ID).ConsumedAt != nil {
		t.Fatal("user insert failure must not consume the proof")
	}
	if txns.last == nil || txns.last.committed {
		t.Fatal("db failure must not commit")
	}
}

func TestCompleteSignupInvalidProof(t *testing.T) {
	ctx := context.Background()
	svc, world, _, _ := newTestAccount(t)
	if _, err := svc.CompleteSignup(ctx, CompleteSignupInput{SignupProof: "not-a-proof"}); !errors.Is(err, errInvalidSignupProof) {
		t.Fatalf("err = %v, want invalid proof", err)
	}
	if world.userCount() != 0 {
		t.Fatal("invalid proof must not create a user")
	}
}

func newTestAccount(t *testing.T) (*AccountCreation, *memAccountStore, *memAccountTransactor, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	world := newMemAccountStore()
	txns := &memAccountTransactor{store: world}
	proofs, err := NewSignupProofs(world, SignupProofPolicy{TTL: 15 * time.Minute}, clock)
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := NewPasswords(newMemPasswordStore(), testPasswordPolicy(), clock)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewAccountCreation(proofs, passwords, world, txns, clock)
	if err != nil {
		t.Fatal(err)
	}
	return svc, world, txns, now
}

type memAccountStore struct {
	mu           sync.Mutex
	proofs       map[ID]SignupProof
	users        map[ID]User
	idents       map[ID]UserIdentifier
	creds        map[ID]PasswordCredential
	failUser     bool
	failPassword bool
}

func newMemAccountStore() *memAccountStore {
	return &memAccountStore{
		proofs: make(map[ID]SignupProof),
		users:  make(map[ID]User),
		idents: make(map[ID]UserIdentifier),
		creds:  make(map[ID]PasswordCredential),
	}
}

func (m *memAccountStore) mustIssue(t *testing.T, kind IdentifierKind, dest string, now time.Time) IssuedSignupProof {
	t.Helper()
	proofs, err := NewSignupProofs(m, SignupProofPolicy{TTL: 15 * time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	consumed := now.Add(-time.Minute)
	chID := mustID(t)
	ch := VerificationChallenge{
		ID: chID, Kind: kind, Purpose: ChallengeSignup, DestinationCanonical: dest,
		TokenHash: make([]byte, TokenHashSize), CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		ConsumedAt: &consumed, MaxAttempts: 5,
	}
	copy(ch.TokenHash, bytes.Repeat([]byte{0x11}, TokenHashSize))
	issued, err := proofs.Issue(context.Background(), ch)
	if err != nil {
		t.Fatal(err)
	}
	return issued
}

func (m *memAccountStore) InsertSignupProof(_ context.Context, proof SignupProof) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.proofs {
		if existing.ChallengeID == proof.ChallengeID || bytes.Equal(existing.TokenHash, proof.TokenHash) {
			return errInvalidSignupProof
		}
	}
	cloned := proof
	cloned.TokenHash = cloneBytes(proof.TokenHash)
	m.proofs[proof.ID] = cloned
	return nil
}

func (m *memAccountStore) GetSignupProof(_ context.Context, id ID) (SignupProof, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.proofs[id]
	if !ok {
		return SignupProof{}, errNotFound
	}
	cloned := p
	cloned.TokenHash = cloneBytes(p.TokenHash)
	return cloned, nil
}

func (m *memAccountStore) ConsumeSignupProofByTokenHash(_ context.Context, tx transaction, tokenHash []byte, now time.Time) (SignupProof, error) {
	atx, ok := tx.(*memAccountTx)
	if !ok || atx.store != m {
		return SignupProof{}, errUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, p := range m.proofs {
		if !bytes.Equal(p.TokenHash, tokenHash) {
			continue
		}
		if _, staged := atx.consumed[id]; staged {
			return SignupProof{}, errSignupProofConsumed
		}
		if p.Purpose != SignupProofSignup {
			return SignupProof{}, errInvalidSignupProof
		}
		if err := p.rejectUnusable(now); err != nil {
			return SignupProof{}, err
		}
		consumed := now
		p.ConsumedAt = &consumed
		cloned := p
		cloned.TokenHash = cloneBytes(p.TokenHash)
		atx.consumed[id] = cloned
		return cloned, nil
	}
	return SignupProof{}, errInvalidSignupProof
}

func (m *memAccountStore) GetActiveIdentifierTx(_ context.Context, tx transaction, kind IdentifierKind, valueCanonical string) (UserIdentifier, error) {
	atx, ok := tx.(*memAccountTx)
	if !ok || atx.store != m {
		return UserIdentifier{}, errUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ident := range atx.idents {
		if ident.Kind == kind && ident.ValueCanonical == valueCanonical && ident.RevokedAt == nil {
			return cloneIdentifier(ident), nil
		}
	}
	for _, ident := range m.idents {
		if ident.Kind == kind && ident.ValueCanonical == valueCanonical && ident.RevokedAt == nil {
			return cloneIdentifier(ident), nil
		}
	}
	return UserIdentifier{}, errNotFound
}

func (m *memAccountStore) InsertUserTx(_ context.Context, tx transaction, user User) error {
	atx, ok := tx.(*memAccountTx)
	if !ok || atx.store != m {
		return errUnavailable
	}
	if m.failUser {
		return errUnavailable
	}
	if !user.EligibleForSession() {
		return errUnavailable
	}
	atx.users[user.ID] = user
	return nil
}

func (m *memAccountStore) InsertIdentifierTx(_ context.Context, tx transaction, identifier UserIdentifier) error {
	atx, ok := tx.(*memAccountTx)
	if !ok || atx.store != m {
		return errUnavailable
	}
	if err := identifier.Validate(); err != nil {
		return err
	}
	for _, existing := range m.idents {
		if existing.RevokedAt == nil && existing.Kind == identifier.Kind && existing.ValueCanonical == identifier.ValueCanonical {
			return errIdentifierConflict
		}
	}
	for _, existing := range atx.idents {
		if existing.RevokedAt == nil && existing.Kind == identifier.Kind && existing.ValueCanonical == identifier.ValueCanonical {
			return errIdentifierConflict
		}
	}
	atx.idents[identifier.ID] = cloneIdentifier(identifier)
	return nil
}

func (m *memAccountStore) InsertPasswordCredentialTx(_ context.Context, tx transaction, credential PasswordCredential) error {
	atx, ok := tx.(*memAccountTx)
	if !ok || atx.store != m {
		return errUnavailable
	}
	if m.failPassword {
		return errUnavailable
	}
	if !credential.Active() {
		return errInvalidPassword
	}
	atx.creds[credential.UserID] = clonePassword(credential)
	return nil
}

func (m *memAccountStore) putUser(u User) User {
	if u.ID.IsZero() {
		id, err := NewID()
		if err != nil {
			panic(err)
		}
		u.ID = id
	}
	m.users[u.ID] = u
	return u
}

func (m *memAccountStore) putIdent(ident UserIdentifier) {
	m.idents[ident.ID] = cloneIdentifier(ident)
}

func (m *memAccountStore) expireProof(id ID, now time.Time) {
	p := m.proofs[id]
	p.CreatedAt = now.Add(-2 * time.Minute)
	p.ExpiresAt = now.Add(-time.Minute)
	m.proofs[id] = p
}

func (m *memAccountStore) mustProof(t *testing.T, id ID) SignupProof {
	t.Helper()
	p, ok := m.proofs[id]
	if !ok {
		t.Fatalf("proof %v missing", id)
	}
	return p
}

func (m *memAccountStore) mustUser(t *testing.T, id ID) User {
	t.Helper()
	u, ok := m.users[id]
	if !ok {
		t.Fatalf("user %v missing", id)
	}
	return u
}

func (m *memAccountStore) mustActiveIdent(t *testing.T, kind IdentifierKind, dest string) UserIdentifier {
	t.Helper()
	for _, ident := range m.idents {
		if ident.Kind == kind && ident.ValueCanonical == dest && ident.RevokedAt == nil {
			return ident
		}
	}
	t.Fatalf("identifier %s %s missing", kind, dest)
	return UserIdentifier{}
}

func (m *memAccountStore) assertProofConsumedOnce(t *testing.T, id ID) {
	t.Helper()
	p := m.mustProof(t, id)
	if p.ConsumedAt == nil {
		t.Fatal("proof must be consumed")
	}
	n := 0
	for _, proof := range m.proofs {
		if proof.ID == id && proof.ConsumedAt != nil {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("consumed copies = %d, want 1", n)
	}
}

func (m *memAccountStore) userCount() int {
	return len(m.users)
}

type memAccountTransactor struct {
	store *memAccountStore
	last  *memAccountTx
}

func (t *memAccountTransactor) Begin(context.Context) (transaction, error) {
	tx := &memAccountTx{
		store:    t.store,
		consumed: make(map[ID]SignupProof),
		users:    make(map[ID]User),
		idents:   make(map[ID]UserIdentifier),
		creds:    make(map[ID]PasswordCredential),
	}
	t.last = tx
	return tx, nil
}

type memAccountTx struct {
	store      *memAccountStore
	consumed   map[ID]SignupProof
	users      map[ID]User
	idents     map[ID]UserIdentifier
	creds      map[ID]PasswordCredential
	committed  bool
	rolled     bool
}

func (tx *memAccountTx) Exec(context.Context, string, ...any) (int64, error) {
	if tx.committed || tx.rolled {
		return 0, errUnavailable
	}
	return 1, nil
}

func (tx *memAccountTx) Commit(context.Context) error {
	if tx.committed || tx.rolled {
		return errUnavailable
	}
	tx.store.mu.Lock()
	defer tx.store.mu.Unlock()
	for id, p := range tx.consumed {
		cloned := p
		cloned.TokenHash = cloneBytes(p.TokenHash)
		tx.store.proofs[id] = cloned
	}
	for id, u := range tx.users {
		tx.store.users[id] = u
	}
	for id, ident := range tx.idents {
		tx.store.idents[id] = cloneIdentifier(ident)
	}
	for id, cred := range tx.creds {
		tx.store.creds[id] = clonePassword(cred)
	}
	tx.committed = true
	return nil
}

func (tx *memAccountTx) Rollback(context.Context) error {
	if tx.committed {
		return nil
	}
	tx.rolled = true
	tx.consumed = nil
	tx.users = nil
	tx.idents = nil
	tx.creds = nil
	return nil
}
