package listings

import (
	"context"
	"errors"
	"testing"

	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	listing := mustDraft(t)
	if err := p.Create(context.Background(), listing); !errors.Is(err, errUnavailable) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := p.Get(context.Background(), listing.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get err = %v", err)
	}
	if err := p.Update(context.Background(), listing, listing.UpdatedAt); !errors.Is(err, errUnavailable) {
		t.Fatalf("update err = %v", err)
	}
	if _, err := p.ListByOwner(context.Background(), listing.OwnerUserID); !errors.Is(err, errUnavailable) {
		t.Fatalf("list err = %v", err)
	}
}

func TestMapDBErr(t *testing.T) {
	if err := mapDBErr(db.ErrNoRows); !errors.Is(err, errNotFound) {
		t.Fatalf("no rows = %v", err)
	}
	if err := mapDBErr(db.ErrConflict); !errors.Is(err, errConflict) {
		t.Fatalf("conflict = %v", err)
	}
	if err := mapDBErr(db.ErrUnavailable); !errors.Is(err, errUnavailable) {
		t.Fatalf("unavailable = %v", err)
	}
	if err := mapDBErr(context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled = %v", err)
	}
}

func TestMarshalAttributesRejectsNestedObject(t *testing.T) {
	if _, err := marshalAttributes(Attributes{"nested": map[string]any{"a": true}}); !errors.Is(err, errInvalidAttributes) {
		t.Fatalf("err = %v", err)
	}
	b, err := marshalAttributes(Attributes{"condition": "used"})
	if err != nil || len(b) == 0 {
		t.Fatalf("marshal = %q err = %v", b, err)
	}
}
