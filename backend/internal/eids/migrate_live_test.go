package eids

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/internal/platform/db"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

func TestLiveMigration53To54RoundTrip(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, url, 3*time.Second)
	if err != nil {
		t.Skip("PostgreSQL unavailable")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skip("PostgreSQL unavailable")
	}

	dir, err := findMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}
	src, err := iofs.New(os.DirFS(dir), ".")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = m.Close() })

	ver, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		t.Fatal(err)
	}
	if dirty {
		t.Fatalf("dirty at %d", ver)
	}
	if ver < 53 {
		t.Skip("database not at 000053")
	}
	if ver > 54 {
		t.Skip("database past 000054")
	}
	if ver == 53 {
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			t.Fatalf("53->54: %v", err)
		}
	}
	assertTRTables(t, pool, true)

	now := time.Now().UTC()
	v, err := NewPending(mustEIDSID(t), TypeProperty, now)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPostgresStore(pool)
	if err := store.Create(ctx, v); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM eids.tr_signed_decisions`)
		_, _ = pool.Exec(context.Background(), `DELETE FROM eids.subject_refs WHERE verification_id = $1`, v.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM eids.verifications WHERE id = $1`, v.ID)
	})
	ref, err := NewSubjectRef()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BindSubject(ctx, SubjectBinding{SubjectRef: ref, VerificationID: v.ID, ListingID: v.ListingID, VerificationType: TypeProperty, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	rec := ReplayRecord{
		DecisionID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ClaimsHash: [32]byte{1}, SubjectRef: ref,
		VerificationType: TypeProperty, Status: "approved", IssuedAt: now, ValidUntil: now.Add(time.Hour),
		KeyID: "tr-v1", ReceivedAt: now, AppliedAt: now,
	}
	if err := store.InsertReplay(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertReplay(ctx, rec); !errors.Is(err, errConflict) {
		t.Fatalf("dup insert=%v", err)
	}
	conflict := rec
	conflict.ClaimsHash = [32]byte{2}
	if err := store.InsertReplay(ctx, conflict); !errors.Is(err, errConflict) {
		t.Fatalf("conflict insert=%v", err)
	}

	marker := mustEIDSID(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO notifications.push_endpoints (
			id, user_id, channel, platform, provider, endpoint_hash, endpoint_key_id,
			endpoint_nonce, endpoint_ciphertext, created_at, updated_at, last_seen_at
		) VALUES (
			$1,$1,'web_push','web','webpush', decode('11','hex') || decode(repeat('22',31),'hex'),
			'v1', decode(repeat('33',12),'hex'), decode(repeat('44',16),'hex'), $2,$2,$2
		)`, marker, now); err != nil {
		// push_endpoints may require 32-byte hash; skip survival insert if shape differs
		t.Logf("push endpoint marker skipped: %v", err)
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("54->53: %v", err)
	}
	assertTRTables(t, pool, false)
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM eids.verifications WHERE id = $1`, v.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("verification lost n=%d err=%v", n, err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("re-up: %v", err)
	}
	assertTRTables(t, pool, true)
	ver, dirty, err = m.Version()
	if err != nil {
		t.Fatal(err)
	}
	if ver != 54 || dirty {
		t.Fatalf("version=%d dirty=%v", ver, dirty)
	}
}

func assertTRTables(t *testing.T, pool *db.Pool, want bool) {
	t.Helper()
	ctx := context.Background()
	for _, name := range []string{"subject_refs", "tr_signed_decisions"} {
		var exists bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'eids' AND table_name = $1
			)`, name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != want {
			t.Fatalf("%s exists=%v want=%v", name, exists, want)
		}
	}
}

func findMigrationsDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		for _, rel := range []string{"migrations", filepath.Join("backend", "migrations")} {
			candidate := filepath.Join(dir, rel)
			if _, err := os.Stat(filepath.Join(candidate, "000001_enable_postgis.up.sql")); err == nil {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fs.ErrNotExist
}
