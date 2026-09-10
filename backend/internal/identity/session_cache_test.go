package identity

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"backend/internal/platform/cache"
)

func TestSessionHotCacheMissLoadsAndPopulates(t *testing.T) {
	ctx := context.Background()
	svc, store, hot, _ := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Resolve(ctx, issued.RawToken)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ID != issued.Session.ID {
		t.Fatalf("session = %v", got.ID)
	}
	if store.hashLookups != 1 {
		t.Fatalf("hashLookups = %d, want 1", store.hashLookups)
	}
	key := sessionHotKey(issued.Session.TokenHash)
	if _, ok := hot.kv[key]; !ok {
		t.Fatal("expected cache populate")
	}
	if !strings.HasPrefix(key, sessionHotKeyPrefix) {
		t.Fatalf("key prefix = %q", key)
	}
	hexPart := strings.TrimPrefix(key, sessionHotKeyPrefix)
	if hexPart != hex.EncodeToString(issued.Session.TokenHash) {
		t.Fatal("cache key must be sha256 token hash hex")
	}
}

func TestSessionHotCacheHitAvoidsDB(t *testing.T) {
	ctx := context.Background()
	svc, store, _, _ := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	if store.hashLookups != 1 {
		t.Fatalf("hashLookups = %d, want 1 (hit avoids DB)", store.hashLookups)
	}
}

func TestMalformedSessionHotCacheFallsBackToDB(t *testing.T) {
	ctx := context.Background()
	svc, store, hot, _ := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	key := sessionHotKey(issued.Session.TokenHash)
	hot.kv[key] = `{"not":"a-session"`
	got, err := svc.Resolve(ctx, issued.RawToken)
	if err != nil {
		t.Fatalf("fallback Resolve: %v", err)
	}
	if got.ID != issued.Session.ID {
		t.Fatal("fallback must load durable session")
	}
	if store.hashLookups != 2 {
		t.Fatalf("hashLookups = %d, want 2", store.hashLookups)
	}
}

func TestRevokedCachedSessionRejected(t *testing.T) {
	ctx := context.Background()
	svc, store, hot, now := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	key := sessionHotKey(issued.Session.TokenHash)
	revoked := now().Format(time.RFC3339Nano)
	var rec hotSessionRecord
	if err := json.Unmarshal([]byte(hot.kv[key]), &rec); err != nil {
		t.Fatal(err)
	}
	rec.RevokedAt = &revoked
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	hot.kv[key] = string(b)
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errSessionRevoked {
		t.Fatalf("err = %v, want %v", err, errSessionRevoked)
	}
	if store.hashLookups != 1 {
		t.Fatalf("rejected cache must not hit DB: hashLookups=%d", store.hashLookups)
	}
}

func TestExpiredCachedSessionRejected(t *testing.T) {
	ctx := context.Background()
	svc, store, hot, now := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	key := sessionHotKey(issued.Session.TokenHash)
	var rec hotSessionRecord
	if err := json.Unmarshal([]byte(hot.kv[key]), &rec); err != nil {
		t.Fatal(err)
	}
	rec.IdleExpiresAt = now().Format(time.RFC3339Nano)
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	hot.kv[key] = string(b)
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errSessionIdleExpired {
		t.Fatalf("err = %v, want %v", err, errSessionIdleExpired)
	}
	if store.hashLookups != 1 {
		t.Fatalf("expired cache must not hit DB: hashLookups=%d", store.hashLookups)
	}
}

func TestRevokeInvalidatesSessionHotCache(t *testing.T) {
	ctx := context.Background()
	svc, store, hot, _ := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, issued.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := hot.kv[sessionHotKey(issued.Session.TokenHash)]; ok {
		t.Fatal("revoke must delete session hot key")
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errSessionRevoked {
		t.Fatalf("err = %v, want %v", err, errSessionRevoked)
	}
}

func TestRevokeAllInvalidatesViaUserEpochWithoutScan(t *testing.T) {
	ctx := context.Background()
	svc, store, hot, _ := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeAllForUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if hot.kv[sessionEpochKey(user.ID)] != "1" {
		t.Fatalf("cached epoch = %q, want durable 1", hot.kv[sessionEpochKey(user.ID)])
	}
	if hot.incrKeys[sessionEpochKey(user.ID)] != 0 {
		t.Fatal("durable epoch must not be owned by Valkey INCR")
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errSessionRevoked {
		t.Fatalf("err = %v, want %v", err, errSessionRevoked)
	}
	if store.hashLookups != 2 {
		t.Fatalf("stale epoch must fall back to DB: hashLookups=%d", store.hashLookups)
	}
}

func TestRevokeAllSucceedsWhenValkeyFails(t *testing.T) {
	ctx := context.Background()
	svc, store, hot, _ := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	hot.unavailable = true
	if err := svc.RevokeAllForUser(ctx, user.ID); err != nil {
		t.Fatalf("durable revoke-all must ignore Valkey: %v", err)
	}
	if store.users[user.ID].SessionEpoch != 1 {
		t.Fatalf("session_epoch = %d, want 1", store.users[user.ID].SessionEpoch)
	}
	if store.mustSession(t, issued.Session.ID).RevokedAt == nil {
		t.Fatal("session must be revoked in PostgreSQL")
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errSessionRevoked {
		t.Fatalf("err = %v, want %v", err, errSessionRevoked)
	}
}

func TestStaleCachedEpochCannotAuthorizeRevokedSession(t *testing.T) {
	ctx := context.Background()
	svc, store, hot, _ := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	sessionKey := sessionHotKey(issued.Session.TokenHash)
	staleSession := hot.kv[sessionKey]
	hot.failSet = true
	if err := svc.RevokeAllForUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	hot.failSet = false
	hot.kv[sessionKey] = staleSession
	hot.kv[sessionEpochKey(user.ID)] = "0"
	if _, err := svc.Resolve(ctx, issued.RawToken); err != errSessionRevoked {
		t.Fatalf("stale Valkey epoch must not authorize: %v", err)
	}
}

func TestSessionHotCacheUnavailableFallsBackToDB(t *testing.T) {
	ctx := context.Background()
	svc, store, hot, _ := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	hot.unavailable = true
	got, err := svc.Resolve(ctx, issued.RawToken)
	if err != nil {
		t.Fatalf("Resolve with cache down: %v", err)
	}
	if got.ID != issued.Session.ID {
		t.Fatal("valid session must resolve from PostgreSQL")
	}
	if store.hashLookups != 1 {
		t.Fatalf("hashLookups = %d", store.hashLookups)
	}
}

func TestRawSessionTokenNeverEntersHotCache(t *testing.T) {
	ctx := context.Background()
	svc, store, hot, _ := newTestSessionsWithCache(t)
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	if issued.RawToken == "" {
		t.Fatal("raw token required for assertion")
	}
	for k, v := range hot.kv {
		if strings.Contains(k, issued.RawToken) || bytes.Contains([]byte(k), []byte(issued.RawToken)) {
			t.Fatalf("raw token in cache key %q", k)
		}
		if strings.Contains(v, issued.RawToken) {
			t.Fatal("raw token in cache value")
		}
		if strings.Contains(v, "password") || strings.Contains(v, "webauthn") {
			t.Fatal("cache value must not include credential material fields")
		}
	}
}

func TestSessionHotCacheTTLBoundedByAbsoluteExpiry(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store := newMemStore()
	hot := newMemHotCache()
	svc, err := NewSessions(store, SessionPolicy{Idle: time.Hour, Absolute: 24 * time.Hour}, hot, SessionCachePolicy{MaxTTL: 5 * time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	user := store.putUser(t, User{})
	issued, err := svc.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, issued.RawToken); err != nil {
		t.Fatal(err)
	}
	if hot.lastTTL != 5*time.Minute {
		t.Fatalf("ttl = %s, want injected max (remaining absolute is 24h)", hot.lastTTL)
	}

	hot2 := newMemHotCache()
	svc2, err := NewSessions(store, SessionPolicy{Idle: time.Hour, Absolute: 24 * time.Hour}, hot2, SessionCachePolicy{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	issued2, err := svc2.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.Resolve(ctx, issued2.RawToken); err != nil {
		t.Fatal(err)
	}
	if hot2.lastTTL != 24*time.Hour {
		t.Fatalf("ttl = %s, want remaining absolute", hot2.lastTTL)
	}

	hot3 := newMemHotCache()
	svc3, err := NewSessions(store, SessionPolicy{Idle: 24 * time.Hour, Absolute: 24 * time.Hour}, hot3, SessionCachePolicy{MaxTTL: time.Hour}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	issued3, err := svc3.Create(ctx, user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	nearAbs := now.Add(24*time.Hour - 30*time.Second)
	svc3.now = func() time.Time { return nearAbs }
	if _, err := svc3.Resolve(ctx, issued3.RawToken); err != nil {
		t.Fatal(err)
	}
	if hot3.lastTTL != 30*time.Second {
		t.Fatalf("ttl = %s, want remaining absolute 30s", hot3.lastTTL)
	}
}

func newTestSessionsWithCache(t *testing.T) (*Sessions, *memStore, *memHotCache, func() time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store := newMemStore()
	hot := newMemHotCache()
	svc, err := NewSessions(store, SessionPolicy{Idle: time.Hour, Absolute: 24 * time.Hour}, hot, SessionCachePolicy{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, hot, clock
}

type memHotCache struct {
	kv          map[string]string
	incrKeys    map[string]int64
	unavailable bool
	failSet     bool
	lastTTL     time.Duration
}

func newMemHotCache() *memHotCache {
	return &memHotCache{kv: make(map[string]string), incrKeys: make(map[string]int64)}
}

func (m *memHotCache) check() error {
	if m.unavailable {
		return cache.ErrUnavailable
	}
	return nil
}

func (m *memHotCache) Get(_ context.Context, key string) (string, error) {
	if err := m.check(); err != nil {
		return "", err
	}
	v, ok := m.kv[key]
	if !ok {
		return "", cache.ErrMiss
	}
	return v, nil
}

func (m *memHotCache) Set(_ context.Context, key, value string, ttl time.Duration) error {
	if err := m.check(); err != nil {
		return err
	}
	if m.failSet {
		return cache.ErrUnavailable
	}
	m.kv[key] = value
	m.lastTTL = ttl
	return nil
}

func (m *memHotCache) Delete(_ context.Context, key string) error {
	if err := m.check(); err != nil {
		return err
	}
	delete(m.kv, key)
	return nil
}

func (m *memHotCache) Increment(_ context.Context, key string, ttl time.Duration) (int64, error) {
	if err := m.check(); err != nil {
		return 0, err
	}
	n := m.incrKeys[key] + 1
	m.incrKeys[key] = n
	m.kv[key] = strconv.FormatInt(n, 10)
	m.lastTTL = ttl
	return n, nil
}

var _ sessionHotCache = (*memHotCache)(nil)

func TestInvalidSessionCachePolicy(t *testing.T) {
	_, err := NewSessions(newMemStore(), SessionPolicy{Idle: time.Hour, Absolute: 24 * time.Hour}, nil, SessionCachePolicy{MaxTTL: -time.Second}, nil)
	if !errors.Is(err, errInvalidPolicy) {
		t.Fatalf("err = %v, want %v", err, errInvalidPolicy)
	}
}
