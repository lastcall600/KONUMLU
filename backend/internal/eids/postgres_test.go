package eids

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	now := time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC)
	v, err := NewPending(mustEIDSID(t), TypeProperty, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Create(context.Background(), v); !errors.Is(err, errUnavailable) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := p.Get(context.Background(), v.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get err = %v", err)
	}
	if _, err := p.GetOpen(context.Background(), v.ListingID, TypeProperty); !errors.Is(err, errUnavailable) {
		t.Fatalf("open err = %v", err)
	}
	if _, err := p.GetLatest(context.Background(), v.ListingID, TypeProperty); !errors.Is(err, errUnavailable) {
		t.Fatalf("latest err = %v", err)
	}
	if err := p.Update(context.Background(), v, v.UpdatedAt); !errors.Is(err, errUnavailable) {
		t.Fatalf("update err = %v", err)
	}
	if _, err := p.LookupSubject(context.Background(), "x"); !errors.Is(err, errUnavailable) {
		t.Fatalf("lookup err = %v", err)
	}
	if _, err := p.GetReplay(context.Background(), "x"); !errors.Is(err, errUnavailable) {
		t.Fatalf("replay err = %v", err)
	}
	if _, _, err := p.LatestReplayIssuedAt(context.Background(), "x"); !errors.Is(err, errUnavailable) {
		t.Fatalf("latest issued err = %v", err)
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
