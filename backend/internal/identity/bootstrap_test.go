package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFirstPasskeyDeadlockAccountStates(t *testing.T) {
	ctx := context.Background()
	fix := newTestBootstrap(t)

	// State 1: zero passkeys + password — Allow without re-auth fails (deadlock without this grant).
	if err := fix.passwords.Set(ctx, fix.user.ID, []byte("fallback-password-ok")); err != nil {
		t.Fatal(err)
	}
	if err := fix.b.Allow(ctx, fix.session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("password-only without re-auth: %v", err)
	}
	if err := fix.b.GrantPassword(ctx, fix.session, []byte("fallback-password-ok")); err != nil {
		t.Fatal(err)
	}
	if err := fix.b.Allow(ctx, fix.session, SensitivePasskeyAdd); err != nil {
		t.Fatalf("password re-auth must authorize first passkey add: %v", err)
	}

	// State 2: zero passkeys + no password (fresh signup session).
	fix2 := newTestBootstrap(t)
	if err := fix2.b.Allow(ctx, fix2.session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("passwordless without bootstrap: %v", err)
	}
	if err := fix2.b.GrantSignup(ctx, fix2.session); err != nil {
		t.Fatal(err)
	}
	if err := fix2.b.Allow(ctx, fix2.session, SensitivePasskeyAdd); err != nil {
		t.Fatalf("signup bootstrap must authorize first passkey add: %v", err)
	}

	// State 3: one existing passkey — bootstrap must not apply.
	_ = seedPasskey(t, fix.pstore, fix.user.ID, fix.now())
	if err := fix.b.Allow(ctx, fix.session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("one passkey leftover bootstrap: %v", err)
	}
	if err := fix.b.GrantPassword(ctx, fix.session, []byte("fallback-password-ok")); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("password bootstrap with passkey: %v", err)
	}
	if err := fix.b.GrantSignup(ctx, fix.session); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("signup bootstrap with passkey: %v", err)
	}

	// State 4: multiple passkeys — still no bootstrap.
	fix3 := newTestBootstrap(t)
	_ = seedPasskey(t, fix3.pstore, fix3.user.ID, fix3.now())
	_ = seedPasskey(t, fix3.pstore, fix3.user.ID, fix3.now())
	if err := fix3.b.GrantSignup(ctx, fix3.session); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("multi passkey signup bootstrap: %v", err)
	}
	if err := fix3.b.Allow(ctx, fix3.session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("multi passkey allow: %v", err)
	}
}

func TestPasswordReauthWrongPasswordAndGenericFailure(t *testing.T) {
	ctx := context.Background()
	fix := newTestBootstrap(t)
	if err := fix.passwords.Set(ctx, fix.user.ID, []byte("fallback-password-ok")); err != nil {
		t.Fatal(err)
	}
	if err := fix.b.GrantPassword(ctx, fix.session, []byte("wrong-password")); !errors.Is(err, errUnauthenticated) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := fix.b.Allow(ctx, fix.session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("failed re-auth must not grant: %v", err)
	}
}

func TestPasswordBootstrapCannotAuthorizePasskeyRemove(t *testing.T) {
	ctx := context.Background()
	fix := newTestBootstrap(t)
	if err := fix.passwords.Set(ctx, fix.user.ID, []byte("fallback-password-ok")); err != nil {
		t.Fatal(err)
	}
	if err := fix.b.GrantPassword(ctx, fix.session, []byte("fallback-password-ok")); err != nil {
		t.Fatal(err)
	}
	if err := fix.b.Allow(ctx, fix.session, SensitivePasskeyRemove); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("remove: %v", err)
	}
	if err := fix.b.Allow(ctx, fix.session, SensitiveOperation("identifier_change")); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("unrelated: %v", err)
	}
}

func TestSignupBootstrapBoundToSessionAndNotReusable(t *testing.T) {
	ctx := context.Background()
	fix := newTestBootstrap(t)
	if err := fix.b.GrantSignup(ctx, fix.session); err != nil {
		t.Fatal(err)
	}
	other, err := fix.sessions.Create(ctx, fix.user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := fix.b.Allow(ctx, other.Session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("other session: %v", err)
	}
	if err := fix.b.Allow(ctx, fix.session, SensitivePasskeyAdd); err != nil {
		t.Fatal(err)
	}
}

func TestSignupBootstrapExpiredSessionRejected(t *testing.T) {
	ctx := context.Background()
	fix := newTestBootstrap(t)
	if err := fix.b.GrantSignup(ctx, fix.session); err != nil {
		t.Fatal(err)
	}
	expired := fix.session
	expired.IdleExpiresAt = fix.now().Add(-time.Second)
	if err := fix.b.Allow(ctx, expired, SensitivePasskeyAdd); !errors.Is(err, errSessionIdleExpired) {
		t.Fatalf("expired: %v", err)
	}
	revoked := fix.session
	now := fix.now()
	revoked.RevokedAt = &now
	if err := fix.b.Allow(ctx, revoked, SensitivePasskeyAdd); !errors.Is(err, errSessionRevoked) {
		t.Fatalf("revoked: %v", err)
	}
}

func TestBootstrapConsumeReplayAndAfterFirstPasskey(t *testing.T) {
	ctx := context.Background()
	fix := newTestBootstrap(t)
	if err := fix.b.GrantSignup(ctx, fix.session); err != nil {
		t.Fatal(err)
	}
	if err := fix.b.Allow(ctx, fix.session, SensitivePasskeyAdd); err != nil {
		t.Fatal(err)
	}
	fix.b.Consume(ctx, fix.session.ID)
	if err := fix.b.Allow(ctx, fix.session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("replay after consume: %v", err)
	}
	if _, ok := fix.hot.kv[passkeyBootstrapKey(fix.session.ID)]; ok {
		t.Fatal("consumed key must be deleted")
	}

	fix2 := newTestBootstrap(t)
	if err := fix2.b.GrantSignup(ctx, fix2.session); err != nil {
		t.Fatal(err)
	}
	_ = seedPasskey(t, fix2.pstore, fix2.user.ID, fix2.now())
	if err := fix2.b.Allow(ctx, fix2.session, SensitivePasskeyAdd); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("after first passkey: %v", err)
	}
}

func TestPasswordSignupAccountCannotUseSignupBootstrap(t *testing.T) {
	ctx := context.Background()
	fix := newTestBootstrap(t)
	if err := fix.passwords.Set(ctx, fix.user.ID, []byte("fallback-password-ok")); err != nil {
		t.Fatal(err)
	}
	if err := fix.b.GrantSignup(ctx, fix.session); !errors.Is(err, errStepUpRequired) {
		t.Fatalf("password account signup bootstrap: %v", err)
	}
}

func TestBootstrapUnavailableFailsClosed(t *testing.T) {
	ctx := context.Background()
	fix := newTestBootstrap(t)
	fix.hot.unavailable = true
	if err := fix.b.GrantSignup(ctx, fix.session); !errors.Is(err, errUnavailable) {
		t.Fatalf("grant: %v", err)
	}
	fix.hot.unavailable = false
	if err := fix.b.GrantSignup(ctx, fix.session); err != nil {
		t.Fatal(err)
	}
	fix.hot.unavailable = true
	if err := fix.b.Allow(ctx, fix.session, SensitivePasskeyAdd); !errors.Is(err, errUnavailable) {
		t.Fatalf("allow: %v", err)
	}
}

func TestBootstrapTTLMatchesStepUpPolicy(t *testing.T) {
	ctx := context.Background()
	fix := newTestBootstrap(t)
	if err := fix.b.GrantSignup(ctx, fix.session); err != nil {
		t.Fatal(err)
	}
	if fix.hot.lastTTL != time.Minute {
		t.Fatalf("ttl = %s", fix.hot.lastTTL)
	}
}

type bootstrapFix struct {
	b         *PasskeyBootstrap
	sessions  *Sessions
	passwords *Passwords
	pstore    *memPasskeyStore
	hot       *memHotCache
	user      User
	session   Session
	now       func() time.Time
}

func newTestBootstrap(t *testing.T) bootstrapFix {
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
	hot := newMemHotCache()
	b, err := NewPasskeyBootstrap(hot, passkeys, passwords, StepUpPolicy{TTL: time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	user := store.putUser(t, User{})
	issued, err := sessions.Create(context.Background(), user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return bootstrapFix{
		b:         b,
		sessions:  sessions,
		passwords: passwords,
		pstore:    pstore,
		hot:       hot,
		user:      user,
		session:   issued.Session,
		now:       now,
	}
}
