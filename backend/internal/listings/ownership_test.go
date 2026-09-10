package listings

import (
	"context"
	"errors"
	"testing"

	"backend/internal/listings/contracts"
)

func TestAssertListingOwnedBySuccessAndFailure(t *testing.T) {
	svc, _, _ := mustService(t)
	owner := mustID(t)
	other := mustID(t)
	created, err := svc.CreateDraft(context.Background(), owner, validContent(mustID(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AssertListingOwnedBy(context.Background(), contracts.ID(created.ID), contracts.ID(owner)); err != nil {
		t.Fatal(err)
	}
	ref, err := svc.ResolveListingOwner(context.Background(), contracts.ID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if ref.OwnerUserID != contracts.ID(owner) || ref.Status != string(StatusDraft) {
		t.Fatalf("ref = %+v", ref)
	}
	if err := svc.AssertListingOwnedBy(context.Background(), contracts.ID(created.ID), contracts.ID(other)); !errors.Is(err, contracts.ErrForbidden) {
		t.Fatalf("foreign err = %v", err)
	}
	if err := svc.AssertListingOwnedBy(context.Background(), contracts.ID{}, contracts.ID(owner)); !errors.Is(err, contracts.ErrZeroID) {
		t.Fatalf("zero err = %v", err)
	}
	if _, err := svc.ResolveListingOwner(context.Background(), contracts.ID(mustID(t))); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}
