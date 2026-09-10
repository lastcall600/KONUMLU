package businesses

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/businesses/contracts"
)

func TestServiceCreateOfferedServiceOwnerOnly(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	other := mustID(t)
	profile, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Kafe"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.CreateOfferedService(context.Background(), owner, profile.ID, ServiceContent{Title: "Tur Кафе"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ServiceStatusDraft || got.BusinessID != profile.ID || got.Title != "Tur Кафе" {
		t.Fatalf("svc = %+v", got)
	}
	if _, err := svc.CreateOfferedService(context.Background(), other, profile.ID, ServiceContent{Title: "Hijack"}); !errors.Is(err, errNotFound) {
		t.Fatalf("foreign create err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.GetOwnedOfferedService(context.Background(), other, profile.ID, got.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("foreign get err = %v", err)
	}
}

func TestServiceOfferedServiceLifecycleAndPublic(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	profile, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Kafe"})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := svc.CreateOfferedService(context.Background(), owner, profile.ID, ServiceContent{Title: "Tur"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublicOfferedService(context.Background(), draft.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("draft public err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	active, err := svc.ActivateOfferedService(context.Background(), owner, profile.ID, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublicOfferedService(context.Background(), active.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("active under draft business public err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Activate(context.Background(), owner, profile.ID); err != nil {
		t.Fatal(err)
	}
	pub, err := svc.GetPublicOfferedService(context.Background(), active.ID)
	if err != nil || pub.ID != active.ID {
		t.Fatalf("public = %+v err = %v", pub, err)
	}
	now.now = now.now.Add(time.Minute)
	paused, err := svc.PauseOfferedService(context.Background(), owner, profile.ID, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublicOfferedService(context.Background(), paused.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("paused public err = %v", err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.ActivateOfferedService(context.Background(), owner, profile.ID, paused.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	closed, err := svc.CloseOfferedService(context.Background(), owner, profile.ID, paused.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetPublicOfferedService(context.Background(), closed.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("closed public err = %v", err)
	}
	if _, err := svc.ActivateOfferedService(context.Background(), owner, profile.ID, closed.ID); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("reopen err = %v", err)
	}
}

func TestServiceListPublicOmitsNonActive(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	profile, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Kafe"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Activate(context.Background(), owner, profile.ID); err != nil {
		t.Fatal(err)
	}
	first, err := svc.CreateOfferedService(context.Background(), owner, profile.ID, ServiceContent{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	second, err := svc.CreateOfferedService(context.Background(), owner, profile.ID, ServiceContent{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.ActivateOfferedService(context.Background(), owner, profile.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	owned, err := svc.ListOwnedOfferedServices(context.Background(), owner, profile.ID)
	if err != nil || len(owned) != 2 || owned[0].ID != first.ID || owned[1].ID != second.ID {
		t.Fatalf("owned = %+v err = %v", owned, err)
	}
	pub, err := svc.ListPublicOfferedServices(context.Background(), profile.ID)
	if err != nil || len(pub) != 1 || pub[0].ID != second.ID {
		t.Fatalf("public = %+v err = %v", pub, err)
	}
}

func TestServiceCatalogContract(t *testing.T) {
	svc, _, _ := mustService(t)
	owner := mustID(t)
	profile, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Kafe"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateOfferedService(context.Background(), owner, profile.ID, ServiceContent{Title: "Tur"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := svc.GetService(context.Background(), contracts.ID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if ref.BusinessID != contracts.ID(profile.ID) || ref.Title != "Tur" || ref.Status != string(ServiceStatusDraft) {
		t.Fatalf("ref = %+v", ref)
	}
	list, err := svc.ListServicesForBusiness(context.Background(), contracts.ID(profile.ID))
	if err != nil || len(list) != 1 || list[0].ID != contracts.ID(created.ID) {
		t.Fatalf("list = %+v err = %v", list, err)
	}
}

func TestServiceRejectsCreateOnClosedBusiness(t *testing.T) {
	svc, _, now := mustService(t)
	owner := mustID(t)
	profile, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Kafe"})
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Close(context.Background(), owner, profile.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateOfferedService(context.Background(), owner, profile.ID, ServiceContent{Title: "Tur"}); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("create on closed err = %v", err)
	}
}
