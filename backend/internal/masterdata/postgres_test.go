package masterdata

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	ctx := context.Background()
	now := time.Date(2026, 9, 6, 18, 0, 0, 0, time.UTC)
	cat, err := NewCategory("test.x", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.CreateCategory(ctx, cat); !errors.Is(err, errUnavailable) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := p.GetCategory(ctx, cat.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get err = %v", err)
	}
	if _, err := p.ListPublishedCategories(ctx); !errors.Is(err, errUnavailable) {
		t.Fatalf("list published err = %v", err)
	}
	if _, err := p.GetPublishedSchema(ctx, cat.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("published schema err = %v", err)
	}
	if _, err := p.GetSchemaByCategoryVersion(ctx, cat.ID, 1); !errors.Is(err, errUnavailable) {
		t.Fatalf("schema by version err = %v", err)
	}
	if _, err := p.ListAttributes(ctx, cat.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("list attrs err = %v", err)
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
	if err := mapDBErr(errors.New("sql: connection refused")); !errors.Is(err, errUnavailable) {
		t.Fatalf("other = %v", err)
	}
}

func TestMarshalConstraintsRejectsNestedObject(t *testing.T) {
	if _, err := marshalConstraints(Constraints{"nested": map[string]any{"a": true}}); !errors.Is(err, errInvalidConstraints) {
		t.Fatalf("err = %v", err)
	}
	b, err := marshalConstraints(Constraints{"min": 1})
	if err != nil || len(b) == 0 {
		t.Fatalf("marshal = %q err = %v", b, err)
	}
}
