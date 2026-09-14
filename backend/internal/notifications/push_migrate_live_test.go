package notifications

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

func TestLiveMigration52To53RoundTrip(t *testing.T) {
	pool := liveNotifyPool(t)
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable"
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
		t.Fatalf("schema_migrations dirty at %d", ver)
	}
	if ver < 52 {
		t.Skip("database not at 000052")
	}
	if ver > 53 {
		t.Skip("database past 000053")
	}
	if ver == 52 {
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			t.Fatalf("52->53: %v", err)
		}
	}
	assertPushEndpointPresent(t, pool, true)

	marker, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `
		INSERT INTO notifications.preference_settings (
			user_id, channel, scope_type, scope_key, enabled, created_at, updated_at
		) VALUES ($1,'email','channel','*',true,$2,$2)
		ON CONFLICT DO NOTHING`, marker, now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM notifications.preference_settings WHERE user_id = $1`, marker)
	})

	if err := m.Steps(-1); err != nil {
		t.Fatalf("53->52: %v", err)
	}
	assertPushEndpointPresent(t, pool, false)
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications.preference_settings WHERE user_id = $1`, marker).Scan(&n); err != nil || n != 1 {
		t.Fatalf("000052 data lost on down: n=%d err=%v", n, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications.intents`).Scan(&n); err != nil {
		t.Fatalf("intents missing after down: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications.channel_deliveries`).Scan(&n); err != nil {
		t.Fatalf("channel_deliveries missing after down: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications.inbox_items`).Scan(&n); err != nil {
		t.Fatalf("inbox_items missing after down: %v", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("re-up 52->53: %v", err)
	}
	assertPushEndpointPresent(t, pool, true)
	ver, dirty, err = m.Version()
	if err != nil {
		t.Fatal(err)
	}
	if ver != 53 || dirty {
		t.Fatalf("version=%d dirty=%v", ver, dirty)
	}
}

func assertPushEndpointPresent(t *testing.T, pool *db.Pool, want bool) {
	t.Helper()
	ctx := context.Background()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'notifications' AND table_name = 'push_endpoints'
		)`).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	if exists != want {
		t.Fatalf("push_endpoints exists=%v want=%v", exists, want)
	}
	if !want {
		return
	}
	var uniq, idx int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_indexes
		WHERE schemaname = 'notifications' AND tablename = 'push_endpoints'
		  AND indexname = 'notifications_push_endpoints_active_hash_uidx'`).Scan(&uniq); err != nil || uniq != 1 {
		t.Fatalf("unique hash index=%d err=%v", uniq, err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_indexes
		WHERE schemaname = 'notifications' AND tablename = 'push_endpoints'`).Scan(&idx); err != nil || idx < 3 {
		t.Fatalf("indexes=%d err=%v", idx, err)
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
