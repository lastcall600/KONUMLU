package location

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/platform/db"
)

func TestPostgresStoreRequiresPool(t *testing.T) {
	p := NewPostgresStore(nil)
	loc := mustLocation(t)
	if err := p.Upsert(context.Background(), loc); !errors.Is(err, errUnavailable) {
		t.Fatalf("upsert err = %v", err)
	}
	if _, err := p.GetByListingID(context.Background(), loc.ListingID); !errors.Is(err, errUnavailable) {
		t.Fatalf("get err = %v", err)
	}
	if err := p.DeleteByListingID(context.Background(), loc.ListingID); !errors.Is(err, errUnavailable) {
		t.Fatalf("delete err = %v", err)
	}
}

func TestMapDBErr(t *testing.T) {
	if err := mapDBErr(db.ErrNoRows); !errors.Is(err, errNotFound) {
		t.Fatalf("no rows = %v", err)
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

func TestUpsertSQLLongitudeLatitudeOrder(t *testing.T) {
	if !strings.Contains(upsertListingLocationSQL, writePointSQL) {
		t.Fatalf("upsert missing point SQL: %s", upsertListingLocationSQL)
	}
	if strings.Index(writePointSQL, "$3") > strings.Index(writePointSQL, "$4") {
		t.Fatal("$3 (longitude) must precede $4 (latitude) in ST_MakePoint")
	}
	loc := mustLocation(t)
	lon, lat := pointWriteArgs(loc.Coordinates())
	if lon != loc.Longitude || lat != loc.Latitude {
		t.Fatalf("write args lon=%v lat=%v loc=%+v", lon, lat, loc)
	}
}

func TestScanListingLocationLatLonColumns(t *testing.T) {
	listingID := mustID(t)
	now := time.Date(2026, 9, 6, 15, 0, 0, 0, time.UTC)
	row := fakeRow{
		vals: []any{listingID, (*ID)(nil), 36.621, 29.116, now, now},
	}
	got, err := scanListingLocation(row)
	if err != nil {
		t.Fatal(err)
	}
	if got.Latitude != 36.621 {
		t.Fatalf("latitude column must be ST_Y; got %v", got.Latitude)
	}
	if got.Longitude != 29.116 {
		t.Fatalf("longitude column must be ST_X; got %v", got.Longitude)
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
		case *float64:
			*ptr = r.vals[i].(float64)
		case *time.Time:
			*ptr = r.vals[i].(time.Time)
		default:
			return errors.New("unsupported dest")
		}
	}
	return nil
}

func TestScanListingLocationMapsNoRows(t *testing.T) {
	_, err := scanListingLocation(fakeRow{err: db.ErrNoRows})
	if !errors.Is(err, db.ErrNoRows) {
		t.Fatalf("err = %v", err)
	}
	if err := mapDBErr(err); !errors.Is(err, errNotFound) {
		t.Fatalf("mapped = %v", err)
	}
}

func mustLocation(t *testing.T) ListingLocation {
	t.Helper()
	loc, err := NewListingLocation(mustID(t), Coordinates{Latitude: 36.621, Longitude: 29.116}, nil, time.Date(2026, 9, 6, 15, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
