package savedsearch

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateListGetDeleteSessionOwned(t *testing.T) {
	svc, _ := newTestService(t)
	user := mustID(t)
	q := "bike"
	created, err := svc.Create(context.Background(), user, CreateInput{Name: "Bisiklet", Filters: Filters{Q: &q}})
	if err != nil {
		t.Fatal(err)
	}
	if created.UserID != user || created.Filters.Q == nil || *created.Filters.Q != "bike" {
		t.Fatalf("created = %+v", created)
	}
	got, err := svc.Get(context.Background(), user, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("get = %+v err=%v", got, err)
	}
	list, err := svc.List(context.Background(), user)
	if err != nil || len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	if err := svc.Delete(context.Background(), user, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), user, created.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("after delete err = %v", err)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	svc, _ := newTestService(t)
	owner, other := mustID(t), mustID(t)
	row, err := svc.Create(context.Background(), owner, CreateInput{Name: "Sahip"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), other, row.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("foreign get err = %v", err)
	}
	list, err := svc.List(context.Background(), other)
	if err != nil || len(list) != 0 {
		t.Fatalf("foreign list = %+v err=%v", list, err)
	}
	if err := svc.Delete(context.Background(), other, row.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("foreign delete err = %v", err)
	}
	if _, err := svc.Get(context.Background(), owner, row.ID); err != nil {
		t.Fatalf("owner still has row: %v", err)
	}
}

func TestCreateRejectsInvalidFiltersAndPartialViewport(t *testing.T) {
	svc, _ := newTestService(t)
	user := mustID(t)
	bad := "abc"
	if _, err := svc.Create(context.Background(), user, CreateInput{Name: "Bad", Filters: Filters{MinPrice: &bad}}); !errors.Is(err, errInvalid) {
		t.Fatalf("price err = %v", err)
	}
	if _, err := svc.Create(context.Background(), user, CreateInput{Name: "Geo", Filters: Filters{
		Viewport: &Viewport{North: 1, South: 2, East: 3, West: 1},
	}}); !errors.Is(err, errInvalid) {
		t.Fatalf("viewport err = %v", err)
	}
}

func TestCreateAcceptsCompleteViewport(t *testing.T) {
	svc, _ := newTestService(t)
	user := mustID(t)
	row, err := svc.Create(context.Background(), user, CreateInput{Name: "Fethiye", Filters: Filters{
		Viewport: &Viewport{North: 37, South: 36, East: 30, West: 28},
	}})
	if err != nil || row.Filters.Viewport == nil {
		t.Fatalf("row = %+v err=%v", row, err)
	}
}

func TestCreateRequiresSessionUserID(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Create(context.Background(), ID{}, CreateInput{Name: "No user"}); !errors.Is(err, errZeroID) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateDoesNotKeepCursorFields(t *testing.T) {
	svc, store := newTestService(t)
	user := mustID(t)
	row, err := svc.Create(context.Background(), user, CreateInput{Name: "No cursor", Filters: Filters{Q: strPtr("bike")}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetByUser(context.Background(), user, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Filters.Viewport != nil && (got.Filters.Q == nil) {
		t.Fatalf("unexpected stored row = %+v", got)
	}
}

func newTestService(t *testing.T) (*Service, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	svc, err := NewService(store, func() time.Time { return time.Unix(10, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	return svc, store
}

func strPtr(s string) *string {
	return &s
}
