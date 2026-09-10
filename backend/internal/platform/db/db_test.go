package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/platform/config"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestOpenPoolAppliesBoundsWithoutLeakingURL(t *testing.T) {
	ctx := context.Background()
	_, err := OpenPool(ctx, "not-a-url", config.Pool{MaxConns: 8, MinConns: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "not-a-url") {
		t.Fatalf("leaked: %v", err)
	}
}

func TestOpenRejectsEmptyAndInvalidURLWithoutLeaking(t *testing.T) {
	ctx := context.Background()
	cases := []string{"", "not-a-url", "://broken"}
	for _, url := range cases {
		_, err := Open(ctx, url, time.Second)
		if err == nil {
			t.Fatalf("Open(%q) expected error", url)
		}
		if errors.Is(err, ErrUnavailable) {
			continue
		}
		if !errors.Is(err, errInvalidDatabaseURL) {
			t.Fatalf("Open(%q) err = %v, want invalid DATABASE_URL", url, err)
		}
		msg := err.Error()
		if strings.Contains(msg, url) && url != "" {
			t.Fatalf("error leaked database url: %v", err)
		}
		if strings.Contains(strings.ToLower(msg), "password") {
			t.Fatalf("error leaked credential material: %v", err)
		}
	}
}

func TestScanPostGIS(t *testing.T) {
	if err := scanPostGIS(fakeRow{installed: true}); err != nil {
		t.Fatalf("installed: %v", err)
	}
	if err := scanPostGIS(fakeRow{installed: false}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing extension: err = %v", err)
	}
	if err := scanPostGIS(fakeRow{err: errors.New("boom")}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("scan error: err = %v", err)
	}
}

func TestMapWriteErrUniqueIsConflict(t *testing.T) {
	if err := mapWriteErr(&pgconn.PgError{Code: "23505"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("unique violation err = %v, want %v", err, ErrConflict)
	}
	if err := mapWriteErr(errors.New("boom")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("other err = %v, want %v", err, ErrUnavailable)
	}
}

func TestPoolQueryAndExecWithoutConn(t *testing.T) {
	p := &Pool{}
	if _, err := p.Exec(context.Background(), "SELECT 1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Exec: %v", err)
	}
	if err := p.QueryRow(context.Background(), "SELECT 1").Scan(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("QueryRow: %v", err)
	}
}

type fakeRow struct {
	installed bool
	err       error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("expected 1 dest")
	}
	ptr, ok := dest[0].(*bool)
	if !ok {
		return errors.New("expected *bool")
	}
	*ptr = r.installed
	return nil
}
