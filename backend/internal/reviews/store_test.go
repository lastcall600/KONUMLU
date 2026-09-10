package reviews

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestListForListingFirstPageNewestFirst(t *testing.T) {
	store := NewMemoryStore()
	listing := mustID(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	older := mustSeedReview(t, store, listing, now.Add(-2*time.Hour), "older")
	newer := mustSeedReview(t, store, listing, now.Add(-time.Hour), "newer")
	other := mustID(t)
	mustSeedReview(t, store, other, now, "other listing")

	got, err := store.ListForListing(context.Background(), listing, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].ID != newer.ID || got[1].ID != older.ID {
		t.Fatalf("order = %+v", got)
	}
}

func TestListForListingNextPageNoDuplicatesOrGaps(t *testing.T) {
	store := NewMemoryStore()
	listing := mustID(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	for i := 0; i < 5; i++ {
		mustSeedReview(t, store, listing, now.Add(time.Duration(i)*time.Hour), "r")
	}
	full, err := store.ListForListing(context.Background(), listing, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 5 {
		t.Fatalf("full len = %d", len(full))
	}

	page1, err := store.ListForListing(context.Background(), listing, nil, 2)
	if err != nil || len(page1) != 2 {
		t.Fatalf("page1 = %+v err=%v", page1, err)
	}
	cursor := &listingCursor{CreatedAt: page1[1].CreatedAt, ID: page1[1].ID}
	page2, err := store.ListForListing(context.Background(), listing, cursor, 2)
	if err != nil || len(page2) != 2 {
		t.Fatalf("page2 = %+v err=%v", page2, err)
	}
	cursor = &listingCursor{CreatedAt: page2[1].CreatedAt, ID: page2[1].ID}
	page3, err := store.ListForListing(context.Background(), listing, cursor, 2)
	if err != nil || len(page3) != 1 {
		t.Fatalf("page3 = %+v err=%v", page3, err)
	}

	merged := append(append(append([]Review{}, page1...), page2...), page3...)
	if len(merged) != len(full) {
		t.Fatalf("merged len = %d full = %d", len(merged), len(full))
	}
	seen := map[ID]struct{}{}
	for i, row := range merged {
		if row.ID != full[i].ID {
			t.Fatalf("gap at %d: got %s want %s", i, row.ID, full[i].ID)
		}
		if _, ok := seen[row.ID]; ok {
			t.Fatalf("duplicate %s", row.ID)
		}
		seen[row.ID] = struct{}{}
	}
}

func TestListForListingDeterministicIDTieBreak(t *testing.T) {
	store := NewMemoryStore()
	listing := mustID(t)
	ts := time.Unix(1_700_000_000, 0).UTC()
	a := mustSeedReviewAt(t, store, listing, ts, mustID(t))
	b := mustSeedReviewAt(t, store, listing, ts, mustID(t))
	got, err := store.ListForListing(context.Background(), listing, nil, 20)
	if err != nil || len(got) != 2 {
		t.Fatalf("got = %+v err=%v", got, err)
	}
	wantFirst, wantSecond := a, b
	if bytes.Compare(b.ID[:], a.ID[:]) > 0 {
		wantFirst, wantSecond = b, a
	}
	if got[0].ID != wantFirst.ID || got[1].ID != wantSecond.ID {
		t.Fatalf("tie-break = %s then %s want %s then %s", got[0].ID, got[1].ID, wantFirst.ID, wantSecond.ID)
	}

	page1, err := store.ListForListing(context.Background(), listing, nil, 1)
	if err != nil || len(page1) != 1 || page1[0].ID != wantFirst.ID {
		t.Fatalf("page1 = %+v err=%v", page1, err)
	}
	page2, err := store.ListForListing(context.Background(), listing, &listingCursor{CreatedAt: ts, ID: page1[0].ID}, 1)
	if err != nil || len(page2) != 1 || page2[0].ID != wantSecond.ID {
		t.Fatalf("page2 = %+v err=%v", page2, err)
	}
}

func TestListForListingBoundedLimit(t *testing.T) {
	store := NewMemoryStore()
	listing := mustID(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	for i := 0; i < 5; i++ {
		mustSeedReview(t, store, listing, now.Add(time.Duration(i)*time.Minute), "r")
	}
	got, err := store.ListForListing(context.Background(), listing, nil, 3)
	if err != nil || len(got) != 3 {
		t.Fatalf("got = %+v err=%v", got, err)
	}
}

func TestDecodePublicCursorRejectsGarbage(t *testing.T) {
	listing := mustID(t)
	if _, err := decodePublicCursor("not-a-cursor", listing); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("garbage err = %v", err)
	}
	if _, err := decodePublicCursor("%%%", listing); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("base64 err = %v", err)
	}
	if _, err := decodePublicCursor(base64.RawURLEncoding.EncodeToString([]byte(`{"v":1}`)), listing); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("partial err = %v", err)
	}
	other := mustID(t)
	row := PublicReview{ID: mustID(t), CreatedAt: time.Unix(1_700_000_000, 0).UTC()}
	cur, err := encodePublicCursor(listing, row)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodePublicCursor(cur, other); !errors.Is(err, errInvalidQuery) {
		t.Fatalf("cross-listing err = %v", err)
	}
}

func mustSeedReview(t *testing.T, store *MemoryStore, listing ID, created time.Time, body string) Review {
	t.Helper()
	return mustSeedReviewAt(t, store, listing, created, mustID(t))
}

func mustSeedReviewAt(t *testing.T, store *MemoryStore, listing ID, created time.Time, id ID) Review {
	t.Helper()
	body := "body"
	row := Review{
		ID:                    id,
		VerifiedInteractionID: mustID(t),
		ListingID:             listing,
		ReviewerUserID:        mustID(t),
		ProviderUserID:        mustID(t),
		Body:                  &body,
		ListingAccuracy:       4,
		ProviderService:       3,
		CreatedAt:             created,
		UpdatedAt:             created,
	}
	if err := store.InsertReview(context.Background(), row, nil); err != nil {
		t.Fatal(err)
	}
	return row
}
