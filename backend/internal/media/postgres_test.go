package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	asset := mustPending(t)
	if err := p.Create(context.Background(), asset); !errors.Is(err, errUnavailable) {
		t.Fatalf("create err = %v", err)
	}
	if _, err := p.Get(context.Background(), asset.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get err = %v", err)
	}
	if err := p.Update(context.Background(), asset, asset.UpdatedAt); !errors.Is(err, errUnavailable) {
		t.Fatalf("update err = %v", err)
	}
	if _, err := p.ListByListing(context.Background(), mustID(t)); !errors.Is(err, errUnavailable) {
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
	if err := mapDBErr(errors.New("sql: connection refused")); !errors.Is(err, errUnavailable) {
		t.Fatalf("other = %v", err)
	}
}

func TestScanAssetNullableListing(t *testing.T) {
	a := mustPending(t)
	row := fakeRow{vals: []any{
		a.ID, a.OwnerUserID, (*ID)(nil), string(a.Kind), string(a.Status), a.ObjectKey, (*string)(nil),
		a.OriginalFilename, a.ContentType, a.SizeBytes, a.Width, a.Height, a.SortOrder,
		a.CreatedAt, a.UpdatedAt, a.ReadyAt, a.RejectedAt, a.DeletedAt,
	}}
	got, err := scanAsset(row)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListingID != nil || got.Status != StatusPendingUpload || got.ObjectKey != a.ObjectKey {
		t.Fatalf("got = %+v", got)
	}
}

type fakeRow struct {
	vals []any
	err  error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return errors.New("scan arity")
	}
	for i, d := range dest {
		switch ptr := d.(type) {
		case *ID:
			*ptr = r.vals[i].(ID)
		case **ID:
			*ptr = r.vals[i].(*ID)
		case *string:
			*ptr = r.vals[i].(string)
		case **string:
			*ptr = r.vals[i].(*string)
		case **int64:
			*ptr = r.vals[i].(*int64)
		case **int:
			*ptr = r.vals[i].(*int)
		case *time.Time:
			*ptr = r.vals[i].(time.Time)
		case **time.Time:
			*ptr = r.vals[i].(*time.Time)
		default:
			return errors.New("unsupported dest")
		}
	}
	return nil
}
