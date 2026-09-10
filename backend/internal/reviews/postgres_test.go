package reviews

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	listing := mustID(t)
	if _, err := p.ListForListing(context.Background(), listing, nil, 2); !errors.Is(err, errUnavailable) {
		t.Fatalf("list err = %v", err)
	}
	cursor := &listingCursor{CreatedAt: time.Unix(1_700_000_000, 0).UTC(), ID: mustID(t)}
	if _, err := p.ListForListing(context.Background(), listing, cursor, 2); !errors.Is(err, errUnavailable) {
		t.Fatalf("cursor list err = %v", err)
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
