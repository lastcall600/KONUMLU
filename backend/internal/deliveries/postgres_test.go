package deliveries

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	d, err := CreatePending(validCreate(t), time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Create(context.Background(), d); !errors.Is(err, errUnavailable) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := p.Get(context.Background(), d.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get err = %v", err)
	}
	if _, err := p.GetByTransactionID(context.Background(), d.TransactionID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get by txn err = %v", err)
	}
	if _, err := p.ListForParticipant(context.Background(), d.RequesterUserID); !errors.Is(err, errUnavailable) {
		t.Fatalf("list err = %v", err)
	}
	if err := p.Update(context.Background(), d, d.UpdatedAt); !errors.Is(err, errUnavailable) {
		t.Fatalf("update err = %v", err)
	}
}

func TestMapDBErr(t *testing.T) {
	if err := mapDBErr(db.ErrNoRows); !errors.Is(err, errNotFound) {
		t.Fatalf("no rows = %v", err)
	}
	if err := mapDBErr(db.ErrConflict); !errors.Is(err, errConflict) {
		t.Fatalf("conflict = %v", err)
	}
	if err := mapDBErr(context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled = %v", err)
	}
}

func TestPostgresMigrationSmokeSkippedWithoutDatabase(t *testing.T) {
	t.Skip("PostgreSQL/Docker environment unavailable")
}
