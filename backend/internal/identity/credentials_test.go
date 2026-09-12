package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRemovePasskeyAllowsWhenAnotherPasskeyExists(t *testing.T) {
	ctx := context.Background()
	fix := newTestCredentialGuard(t)
	a := seedPasskey(t, fix.pstore, fix.user.ID, time.Now().UTC())
	_ = seedPasskey(t, fix.pstore, fix.user.ID, time.Now().UTC())
	issued, err := fix.sessions.Create(ctx, fix.user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fix.g.RemovePasskey(ctx, fix.user.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fix.sessions.Resolve(ctx, issued.RawToken); err != errSessionRevoked {
		t.Fatalf("passkey removal must logout-all: %v", err)
	}
}

func TestRemovePasskeyAllowsWhenPasswordFallbackExists(t *testing.T) {
	ctx := context.Background()
	fix := newTestCredentialGuard(t)
	cred := seedPasskey(t, fix.pstore, fix.user.ID, time.Now().UTC())
	if err := fix.g.passwords.Set(ctx, fix.user.ID, []byte("fallback-password-ok")); err != nil {
		t.Fatal(err)
	}
	if err := fix.g.RemovePasskey(ctx, fix.user.ID, cred.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveLastPasskeyWithoutPasswordRejected(t *testing.T) {
	ctx := context.Background()
	fix := newTestCredentialGuard(t)
	cred := seedPasskey(t, fix.pstore, fix.user.ID, time.Now().UTC())
	issued, err := fix.sessions.Create(ctx, fix.user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fix.g.RemovePasskey(ctx, fix.user.ID, cred.ID); !errors.Is(err, errLastCredential) {
		t.Fatalf("err = %v", err)
	}
	if _, err := fix.sessions.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatalf("rejected remove must not revoke sessions: %v", err)
	}
}

func TestRemovePasskeyHidesForeignCredential(t *testing.T) {
	ctx := context.Background()
	fix := newTestCredentialGuard(t)
	other := fix.store.putUser(t, User{})
	cred := seedPasskey(t, fix.pstore, other.ID, time.Now().UTC())
	if err := fix.g.RemovePasskey(ctx, fix.user.ID, cred.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemovePasskeyIdempotentWhenAlreadyRevoked(t *testing.T) {
	ctx := context.Background()
	fix := newTestCredentialGuard(t)
	a := seedPasskey(t, fix.pstore, fix.user.ID, time.Now().UTC())
	_ = seedPasskey(t, fix.pstore, fix.user.ID, time.Now().UTC())
	if err := fix.g.RemovePasskey(ctx, fix.user.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := fix.g.RemovePasskey(ctx, fix.user.ID, a.ID); err != nil {
		t.Fatalf("replay: %v", err)
	}
}

type credentialFix struct {
	g        *CredentialGuard
	sessions *Sessions
	store    *memStore
	pstore   *memPasskeyStore
	user     User
}

func newTestCredentialGuard(t *testing.T) credentialFix {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC) }
	store := newMemStore()
	sessions := mustSessions(t, store, now)
	pstore := newMemPasskeyStore()
	passkeys, err := NewPasskeys(pstore, now)
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := NewPasswords(store, testPasswordPolicy(), now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewCredentialGuard(passkeys, passwords, sessions)
	if err != nil {
		t.Fatal(err)
	}
	user := store.putUser(t, User{})
	return credentialFix{g: g, sessions: sessions, store: store, pstore: pstore, user: user}
}

func seedPasskey(t *testing.T, pstore *memPasskeyStore, userID ID, at time.Time) PasskeyCredential {
	t.Helper()
	cred := validPasskey(t, at)
	cred.UserID = userID
	id := mustID(t)
	cred.CredentialID = id[:]
	if err := pstore.InsertPasskey(context.Background(), cred); err != nil {
		t.Fatal(err)
	}
	return cred
}
