package moderation

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	reporter := mustID(t)
	if err := p.Insert(context.Background(), Report{}); !errors.Is(err, errUnavailable) {
		t.Fatalf("insert err = %v", err)
	}
	if _, err := p.FindRecentIdentical(context.Background(), reporter, TargetListing, mustID(t), ReasonSpam, time.Unix(1, 0).UTC()); !errors.Is(err, errUnavailable) {
		t.Fatalf("find err = %v", err)
	}
	if _, err := p.ListForReporter(context.Background(), reporter, 2); !errors.Is(err, errUnavailable) {
		t.Fatalf("list err = %v", err)
	}
	if _, err := p.GetByID(context.Background(), reporter); !errors.Is(err, errUnavailable) {
		t.Fatalf("get err = %v", err)
	}
	if _, err := p.ListQueue(context.Background(), QueueQuery{}, nil, 2); !errors.Is(err, errUnavailable) {
		t.Fatalf("queue err = %v", err)
	}
	if err := p.UpdateStatus(context.Background(), reporter, StatusSubmitted, StatusTriaged, time.Unix(1, 0).UTC(), nil, nil); !errors.Is(err, errUnavailable) {
		t.Fatalf("update err = %v", err)
	}
	if err := p.InsertCase(context.Background(), Case{}, nil, nil); !errors.Is(err, errUnavailable) {
		t.Fatalf("insert case err = %v", err)
	}
	if _, err := p.GetCase(context.Background(), reporter); !errors.Is(err, errUnavailable) {
		t.Fatalf("get case err = %v", err)
	}
	if _, err := p.ListCases(context.Background(), CaseQuery{}, nil, 2); !errors.Is(err, errUnavailable) {
		t.Fatalf("list cases err = %v", err)
	}
	if err := p.InsertEvidence(context.Background(), CaseEvidence{}, CaseHistory{}); !errors.Is(err, errUnavailable) {
		t.Fatalf("insert evidence err = %v", err)
	}
	if _, err := p.ListEvidence(context.Background(), reporter, 2); !errors.Is(err, errUnavailable) {
		t.Fatalf("list evidence err = %v", err)
	}
	if err := p.InsertAction(context.Background(), CaseAction{}, CaseHistory{}); !errors.Is(err, errUnavailable) {
		t.Fatalf("insert action err = %v", err)
	}
	if _, err := p.GetAction(context.Background(), reporter); !errors.Is(err, errUnavailable) {
		t.Fatalf("get action err = %v", err)
	}
	if _, err := p.ListActions(context.Background(), reporter, 2); !errors.Is(err, errUnavailable) {
		t.Fatalf("list actions err = %v", err)
	}
	if err := p.UpdateAction(context.Background(), CaseAction{}, CaseAction{}, CaseHistory{}); !errors.Is(err, errUnavailable) {
		t.Fatalf("update action err = %v", err)
	}
	if err := p.WithTx(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, errUnavailable) {
		t.Fatalf("with tx err = %v", err)
	}
	if err := p.InsertAppeal(context.Background(), Appeal{}, CaseHistory{}); !errors.Is(err, errUnavailable) {
		t.Fatalf("insert appeal err = %v", err)
	}
	if _, err := p.GetAppeal(context.Background(), reporter); !errors.Is(err, errUnavailable) {
		t.Fatalf("get appeal err = %v", err)
	}
	if _, err := p.FindActiveAppeal(context.Background(), reporter, reporter); !errors.Is(err, errUnavailable) {
		t.Fatalf("find active appeal err = %v", err)
	}
	if _, err := p.ListAppeals(context.Background(), reporter, 2); !errors.Is(err, errUnavailable) {
		t.Fatalf("list appeals err = %v", err)
	}
	if _, err := p.ListAppealsForAppellant(context.Background(), reporter, 2); !errors.Is(err, errUnavailable) {
		t.Fatalf("list mine appeals err = %v", err)
	}
	if err := p.UpdateAppeal(context.Background(), Appeal{}, Appeal{}, nil); !errors.Is(err, errUnavailable) {
		t.Fatalf("update appeal err = %v", err)
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
