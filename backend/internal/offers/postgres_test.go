package offers

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	offer, err := Submit(mustID(t), validContent(), time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Create(context.Background(), offer); !errors.Is(err, errUnavailable) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := p.Get(context.Background(), offer.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get err = %v", err)
	}
	if _, err := p.ListByNeed(context.Background(), offer.NeedID); !errors.Is(err, errUnavailable) {
		t.Fatalf("list need err = %v", err)
	}
	if _, err := p.ListByProvider(context.Background(), offer.ProviderUserID); !errors.Is(err, errUnavailable) {
		t.Fatalf("list provider err = %v", err)
	}
	if _, err := p.FindSubmitted(context.Background(), offer.NeedID, offer.ServiceID); !errors.Is(err, errUnavailable) {
		t.Fatalf("find err = %v", err)
	}
	if err := p.Update(context.Background(), offer, offer.UpdatedAt); !errors.Is(err, errUnavailable) {
		t.Fatalf("update err = %v", err)
	}
	if _, err := p.AcceptExclusive(context.Background(), offer.ID, offer.NeedID, offer.CreatedAt); !errors.Is(err, errUnavailable) {
		t.Fatalf("accept err = %v", err)
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
