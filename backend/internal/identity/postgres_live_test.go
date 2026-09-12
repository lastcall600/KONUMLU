package identity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"backend/internal/platform/db"
)

func TestLivePostgresSessionAndPasskeyOwnership(t *testing.T) {
	pool := liveIdentityPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
	now := func() time.Time { return time.Now().UTC() }
	sessions, err := NewSessions(store, SessionPolicy{Idle: time.Hour, Absolute: 24 * time.Hour}, nil, SessionCachePolicy{}, now)
	if err != nil {
		t.Fatal(err)
	}

	user, err := insertLiveUser(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupLiveUser(context.Background(), pool, user) })
	other, err := insertLiveUser(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupLiveUser(context.Background(), pool, other) })

	a, err := sessions.Create(ctx, user, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := sessions.Create(ctx, user, nil)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := sessions.Create(ctx, other, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Resolve(ctx, a.RawToken); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.GetOwned(ctx, user, foreign.Session.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("foreign get = %v", err)
	}
	list, err := sessions.ListActiveForUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list = %d", len(list))
	}
	if err := sessions.RevokeOthers(ctx, user, a.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Resolve(ctx, b.RawToken); err != errSessionRevoked {
		t.Fatalf("revoked other = %v", err)
	}
	if _, err := sessions.Resolve(ctx, a.RawToken); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Resolve(ctx, foreign.RawToken); err != nil {
		t.Fatal(err)
	}

	passkeys, err := NewPasskeys(store, now)
	if err != nil {
		t.Fatal(err)
	}
	cred := livePasskey(t, user, now())
	if err := passkeys.Create(ctx, cred); err != nil {
		t.Fatal(err)
	}
	listed, err := passkeys.ListActiveForUser(ctx, user)
	if err != nil || len(listed) != 1 || listed[0].ID != cred.ID {
		t.Fatalf("passkeys = %+v err=%v", listed, err)
	}
	if err := passkeys.Revoke(ctx, cred.ID); err != nil {
		t.Fatal(err)
	}
	listed, err = passkeys.ListActiveForUser(ctx, other)
	if err != nil || len(listed) != 0 {
		t.Fatalf("other passkeys = %+v err=%v", listed, err)
	}
}

func TestLivePostgresCeremonyConsumeOnce(t *testing.T) {
	pool := liveIdentityPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
	ceremonies, err := NewCeremonies(store, CeremonyPolicy{TTL: time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := ceremonies.Create(ctx, CeremonyAuthentication, nil, []byte(`{"challenge":"live"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ceremonies.Consume(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	if _, err := ceremonies.Consume(ctx, issued.RawToken); !errors.Is(err, errCeremonyConsumed) {
		t.Fatalf("second consume = %v", err)
	}

	second, err := ceremonies.Create(ctx, CeremonyAuthentication, nil, []byte(`{"challenge":"live-race"}`))
	if err != nil {
		t.Fatal(err)
	}
	var ok, consumed int
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := ceremonies.Consume(ctx, second.RawToken)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ok++
			} else if errors.Is(err, errCeremonyConsumed) {
				consumed++
			}
		}()
	}
	wg.Wait()
	if ok != 1 || consumed != 1 {
		t.Fatalf("concurrent consume ok=%d consumed=%d", ok, consumed)
	}
}

func TestLivePostgresRevokeTouchRace(t *testing.T) {
	pool := liveIdentityPool(t)
	ctx := context.Background()
	store := NewPostgresStore(pool)
	sessions, err := NewSessions(store, SessionPolicy{Idle: time.Hour, Absolute: 24 * time.Hour}, nil, SessionCachePolicy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	user, err := insertLiveUser(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupLiveUser(context.Background(), pool, user) })
	issued, err := sessions.Create(ctx, user, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = sessions.Revoke(ctx, issued.Session.ID)
	}()
	go func() {
		defer wg.Done()
		_ = sessions.Touch(ctx, issued.Session.ID)
	}()
	wg.Wait()
	if _, err := sessions.Resolve(ctx, issued.RawToken); err != errSessionRevoked {
		t.Fatalf("resolve after race = %v", err)
	}
}

func liveIdentityPool(t *testing.T) *db.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, url, 2*time.Second)
	if err != nil {
		t.Skip("postgres not configured")
	}
	if err := pool.Ready(ctx); err != nil {
		pool.Close()
		t.Skip("local postgres/postgis not reachable")
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertLiveUser(ctx context.Context, pool *db.Pool) (ID, error) {
	id, err := NewID()
	if err != nil {
		return ID{}, err
	}
	now := time.Now().UTC()
	_, err = pool.Exec(ctx, `
		INSERT INTO identity.users (id, created_at, updated_at, session_epoch)
		VALUES ($1, $2, $2, 0)`, id, now)
	return id, err
}

func cleanupLiveUser(ctx context.Context, pool *db.Pool, userID ID) {
	_, _ = pool.Exec(ctx, `DELETE FROM identity.sessions WHERE user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM identity.passkey_credentials WHERE user_id = $1`, userID)
	_, _ = pool.Exec(ctx, `DELETE FROM identity.users WHERE id = $1`, userID)
}

func livePasskey(t *testing.T, userID ID, at time.Time) PasskeyCredential {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	credID := make([]byte, 32)
	copy(credID, id[:])
	return PasskeyCredential{
		ID:           id,
		UserID:       userID,
		CredentialID: credID,
		PublicKey:    bytes.Repeat([]byte{1}, 32),
		CreatedAt:    at,
	}
}

