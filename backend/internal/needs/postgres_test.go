package needs

import (
	"context"
	"errors"
	"testing"

	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	need := mustDraft(t)
	if err := p.Create(context.Background(), need); !errors.Is(err, errUnavailable) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := p.Get(context.Background(), need.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get err = %v", err)
	}
	if _, err := p.ListByRequester(context.Background(), need.RequesterUserID); !errors.Is(err, errUnavailable) {
		t.Fatalf("list err = %v", err)
	}
	if err := p.Update(context.Background(), need, need.UpdatedAt); !errors.Is(err, errUnavailable) {
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
