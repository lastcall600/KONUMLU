package businesses

import (
	"context"
	"errors"
	"testing"

	"backend/internal/businesses/contracts"
	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	profile := mustDraft(t)
	if err := p.Create(context.Background(), profile); !errors.Is(err, errUnavailable) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := p.Get(context.Background(), profile.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get err = %v", err)
	}
	if _, err := p.GetByOwner(context.Background(), profile.OwnerUserID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get by owner err = %v", err)
	}
	if err := p.Update(context.Background(), profile, profile.UpdatedAt); !errors.Is(err, errUnavailable) {
		t.Fatalf("update err = %v", err)
	}
	svc := mustDraftService(t)
	if err := p.CreateOfferedService(context.Background(), svc); !errors.Is(err, errUnavailable) {
		t.Fatalf("create service err = %v", err)
	}
	if _, err := p.GetOfferedService(context.Background(), svc.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get service err = %v", err)
	}
	if _, err := p.ListOfferedServices(context.Background(), svc.BusinessID); !errors.Is(err, errUnavailable) {
		t.Fatalf("list services err = %v", err)
	}
	if err := p.UpdateOfferedService(context.Background(), svc, svc.UpdatedAt); !errors.Is(err, errUnavailable) {
		t.Fatalf("update service err = %v", err)
	}
	if _, err := p.FindServiceCandidates(context.Background(), contracts.CandidateQuery{
		Latitude: 36.621, Longitude: 29.116, RadiusKm: 10, Limit: 20,
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("find candidates err = %v", err)
	}
	if _, err := p.CheckServiceCandidate(context.Background(), contracts.EligibilityCheck{
		BusinessID: contracts.ID(profile.ID), ServiceID: contracts.ID(svc.ID),
		Latitude: 36.621, Longitude: 29.116, RadiusKm: 10,
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("check candidate err = %v", err)
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
