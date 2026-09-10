package moderation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreInsertAndListOwnedOnly(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(1_700_000_000, 0).UTC()
	reporter := mustID(t)
	other := mustID(t)
	owned := mustSeedReport(t, store, reporter, TargetListing, mustID(t), ReasonSpam, now)
	mustSeedReport(t, store, other, TargetListing, mustID(t), ReasonSpam, now.Add(time.Minute))

	got, err := store.ListForReporter(context.Background(), reporter, 20)
	if err != nil || len(got) != 1 || got[0].ID != owned.ID {
		t.Fatalf("list = %+v err=%v", got, err)
	}
}

func TestMemoryStoreQueueOrderAndUpdateStatus(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(1_700_000_000, 0).UTC()
	reporter := mustID(t)
	older := mustSeedReport(t, store, reporter, TargetListing, mustID(t), ReasonSpam, now)
	newer := mustSeedReport(t, store, reporter, TargetPublicProfile, mustID(t), ReasonHarassment, now.Add(time.Minute))
	st := StatusSubmitted
	newest, err := store.ListQueue(context.Background(), QueueQuery{Status: &st, Order: QueueNewest}, nil, 10)
	if err != nil || len(newest) != 2 || newest[0].ID != newer.ID || newest[1].ID != older.ID {
		t.Fatalf("newest = %+v err=%v", newest, err)
	}
	oldest, err := store.ListQueue(context.Background(), QueueQuery{Order: QueueOldest}, nil, 10)
	if err != nil || len(oldest) != 2 || oldest[0].ID != older.ID {
		t.Fatalf("oldest = %+v err=%v", oldest, err)
	}
	page, err := store.ListQueue(context.Background(), QueueQuery{Order: QueueNewest}, &queueCursor{CreatedAt: newer.CreatedAt, ID: newer.ID, Order: QueueNewest}, 10)
	if err != nil || len(page) != 1 || page[0].ID != older.ID {
		t.Fatalf("cursor page = %+v err=%v", page, err)
	}
	note := "triage"
	actor := mustID(t)
	if err := store.UpdateStatus(context.Background(), older.ID, StatusSubmitted, StatusTriaged, now.Add(2*time.Minute), &note, &actor); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetByID(context.Background(), older.ID)
	if err != nil || got.Status != StatusTriaged || got.StaffNote == nil || *got.StaffNote != "triage" {
		t.Fatalf("updated = %+v err=%v", got, err)
	}
	if got.ReporterUserID != older.ReporterUserID || got.ReasonCode != older.ReasonCode {
		t.Fatalf("immutable mutated: %+v", got)
	}
	if err := store.UpdateStatus(context.Background(), older.ID, StatusSubmitted, StatusClosed, now.Add(3*time.Minute), nil, nil); !errors.Is(err, errConflict) {
		t.Fatalf("stale update err = %v", err)
	}
	tt := TargetPublicProfile
	filtered, err := store.ListQueue(context.Background(), QueueQuery{TargetType: &tt}, nil, 10)
	if err != nil || len(filtered) != 1 || filtered[0].ID != newer.ID {
		t.Fatalf("filter = %+v err=%v", filtered, err)
	}
}

func TestMemoryStoreRecentIdenticalAndOpenConflict(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(1_700_000_000, 0).UTC()
	reporter := mustID(t)
	target := mustID(t)
	first := mustSeedReport(t, store, reporter, TargetPublicProfile, target, ReasonHarassment, now)
	_, err := store.FindRecentIdentical(context.Background(), reporter, TargetPublicProfile, target, ReasonHarassment, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	dup := first
	dup.ID = mustID(t)
	if err := store.Insert(context.Background(), dup); !errors.Is(err, errConflict) {
		t.Fatalf("open duplicate err = %v", err)
	}
	_, err = store.FindRecentIdentical(context.Background(), reporter, TargetPublicProfile, target, ReasonSpam, now.Add(-time.Hour))
	if !errors.Is(err, errNotFound) {
		t.Fatalf("other reason err = %v", err)
	}
}

func mustSeedReport(t *testing.T, store *MemoryStore, reporter ID, targetType TargetType, targetID ID, reason ReasonCode, created time.Time) Report {
	t.Helper()
	row := Report{
		ID:             mustID(t),
		ReporterUserID: reporter,
		TargetType:     targetType,
		TargetID:       targetID,
		ReasonCode:     reason,
		Status:         StatusSubmitted,
		CreatedAt:      created,
		UpdatedAt:      created,
	}
	if err := store.Insert(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	return row
}
