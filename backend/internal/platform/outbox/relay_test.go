package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryRegisterAndDuplicate(t *testing.T) {
	reg := NewRegistry()
	h := HandlerFunc(func(context.Context, Event) error { return nil })
	if err := reg.Register("notify.email", 1, h); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("notify.email", 1, h); !errors.Is(err, ErrDuplicateHandler) {
		t.Fatalf("dup err = %v, want %v", err, ErrDuplicateHandler)
	}
	if err := reg.Register("notify.email", 2, h); err != nil {
		t.Fatalf("different version must be allowed: %v", err)
	}
	if err := reg.Register("notify.sms", 1, h); err != nil {
		t.Fatalf("different type must be allowed: %v", err)
	}
	if err := reg.Register("notify.email", 1, nil); !errors.Is(err, ErrHandlerRequired) {
		t.Fatalf("nil handler err = %v, want %v", err, ErrHandlerRequired)
	}
	if err := reg.Register("", 1, h); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("empty type err = %v", err)
	}
	if _, ok := reg.Lookup("notify.email", 1); !ok {
		t.Fatal("expected registered handler")
	}
	if _, ok := reg.Lookup("notify.email", 9); ok {
		t.Fatal("unknown version must not match")
	}
}

func TestRelayDispatchesMatchingTypeAndVersion(t *testing.T) {
	ctx := context.Background()
	svc, _, now := newTestOutboxWithBatch(t, 8)
	reg := NewRegistry()
	var v1, v2, other int
	mustRegister(t, reg, "notify.email", 1, HandlerFunc(func(context.Context, Event) error {
		v1++
		return nil
	}))
	mustRegister(t, reg, "notify.email", 2, HandlerFunc(func(context.Context, Event) error {
		v2++
		return nil
	}))
	mustRegister(t, reg, "audit.login", 1, HandlerFunc(func(context.Context, Event) error {
		other++
		return nil
	}))
	enqueue(t, svc, "notify.email", 1)
	enqueue(t, svc, "notify.email", 2)
	enqueue(t, svc, "audit.login", 1)

	relay := mustRelay(t, svc, reg)
	if n, err := relay.processBatch(ctx); err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if v1 != 1 || v2 != 1 || other != 1 {
		t.Fatalf("dispatch counts v1=%d v2=%d other=%d", v1, v2, other)
	}
	_ = now
}

func TestRelaySuccessCompletes(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestOutbox(t)
	e := enqueue(t, svc, "t", 1)
	reg := NewRegistry()
	mustRegister(t, reg, "t", 1, HandlerFunc(func(context.Context, Event) error { return nil }))
	if n, err := mustRelay(t, svc, reg).processBatch(ctx); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	row := store.must(t, e.ID)
	if row.CompletedAt == nil {
		t.Fatal("success must MarkComplete")
	}
}

func TestRelayHandlerFailureReschedules(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestOutbox(t)
	e := enqueue(t, svc, "t", 1)
	reg := NewRegistry()
	mustRegister(t, reg, "t", 1, HandlerFunc(func(context.Context, Event) error {
		return errors.New("provider down")
	}))
	if _, err := mustRelay(t, svc, reg).processBatch(ctx); err != nil {
		t.Fatal(err)
	}
	row := store.must(t, e.ID)
	if row.CompletedAt != nil {
		t.Fatal("failure must not complete")
	}
	if row.LastErrorClass == nil || *row.LastErrorClass != ErrorClassHandlerFailed {
		t.Fatalf("error class = %v", row.LastErrorClass)
	}
	if row.ClaimedAt != nil {
		t.Fatal("failure must release lease")
	}
	want, err := testPolicy().NextAvailableAt(now(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !row.AvailableAt.Equal(want) {
		t.Fatalf("available_at = %v want %v", row.AvailableAt, want)
	}
}

func TestRelayUnknownHandlerReschedules(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestOutbox(t)
	e := enqueue(t, svc, "missing.type", 1)
	reg := NewRegistry()
	if _, err := mustRelay(t, svc, reg).processBatch(ctx); err != nil {
		t.Fatal(err)
	}
	row := store.must(t, e.ID)
	if row.CompletedAt != nil {
		t.Fatal("unknown handler must not complete")
	}
	if row.LastErrorClass == nil || *row.LastErrorClass != ErrorClassUnknownHandler {
		t.Fatalf("error class = %v", row.LastErrorClass)
	}
}

func TestRelayContinuesAfterOneFailure(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestOutboxWithBatch(t, 8)
	a := enqueue(t, svc, "ok.a", 1)
	b := enqueue(t, svc, "fail.b", 1)
	c := enqueue(t, svc, "ok.c", 1)
	reg := NewRegistry()
	mustRegister(t, reg, "ok.a", 1, HandlerFunc(func(context.Context, Event) error { return nil }))
	mustRegister(t, reg, "fail.b", 1, HandlerFunc(func(context.Context, Event) error { return errors.New("boom") }))
	mustRegister(t, reg, "ok.c", 1, HandlerFunc(func(context.Context, Event) error { return nil }))

	if n, err := mustRelay(t, svc, reg).processBatch(ctx); err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if store.must(t, a.ID).CompletedAt == nil || store.must(t, c.ID).CompletedAt == nil {
		t.Fatal("successful events in the batch must complete")
	}
	if store.must(t, b.ID).CompletedAt != nil {
		t.Fatal("failed event must not complete")
	}
}

func TestRelayHandlerRunsAfterClaimReturns(t *testing.T) {
	ctx := context.Background()
	inner := newMemStore()
	ordered := &claimOrderStore{inner: inner}
	now := func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	svc, err := New(ordered, testPolicy(), now)
	if err != nil {
		t.Fatal(err)
	}
	enqueue(t, svc, "t", 1)
	reg := NewRegistry()
	mustRegister(t, reg, "t", 1, HandlerFunc(func(context.Context, Event) error {
		if !ordered.claimDone.Load() {
			t.Error("handler must run after claim/DB transaction completes")
		}
		return nil
	}))
	if _, err := mustRelay(t, svc, reg).processBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if !ordered.claimDone.Load() {
		t.Fatal("claim must complete")
	}
}

func TestRelayCancellationStopsLoop(t *testing.T) {
	svc, _, _ := newTestOutbox(t)
	relay := mustRelay(t, svc, NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled Run must return")
	}
}

func TestRelayNoEventsWaitsPollInterval(t *testing.T) {
	svc, _, _ := newTestOutbox(t)
	relay := mustRelay(t, svc, NewRegistry())
	var waits atomic.Int32
	var got time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	relay.wait = func(_ context.Context, d time.Duration) error {
		got = d
		if waits.Add(1) >= 1 {
			cancel()
			return context.Canceled
		}
		return nil
	}
	if err := relay.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if waits.Load() < 1 {
		t.Fatal("empty claim must wait instead of busy-looping")
	}
	if got != time.Second {
		t.Fatalf("wait duration = %s, want poll interval", got)
	}
}

func TestRelayRunWorkersCancel(t *testing.T) {
	svc, _, _ := newTestOutbox(t)
	relay := mustRelay(t, svc, NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := relay.RunWorkers(ctx, 2); err != nil {
		t.Fatalf("RunWorkers err = %v", err)
	}
}

func TestNewRelayRejectsInvalidDeps(t *testing.T) {
	svc, _, _ := newTestOutbox(t)
	if _, err := NewRelay(nil, NewRegistry(), time.Second, nil); !errors.Is(err, ErrStoreRequired) {
		t.Fatalf("nil outbox: %v", err)
	}
	if _, err := NewRelay(svc, nil, time.Second, nil); !errors.Is(err, ErrRelayRequired) {
		t.Fatalf("nil registry: %v", err)
	}
	if _, err := NewRelay(svc, NewRegistry(), 0, nil); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("poll: %v", err)
	}
	relay := mustRelay(t, svc, NewRegistry())
	if err := relay.RunWorkers(context.Background(), 0); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("workers: %v", err)
	}
}

func mustRegister(t *testing.T, reg *Registry, eventType string, version int, h Handler) {
	t.Helper()
	if err := reg.Register(eventType, version, h); err != nil {
		t.Fatal(err)
	}
}

func mustRelay(t *testing.T, svc *Outbox, reg *Registry) *Relay {
	t.Helper()
	r, err := NewRelay(svc, reg, time.Second, svc.now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func enqueue(t *testing.T, svc *Outbox, eventType string, version int) Event {
	t.Helper()
	e, err := svc.Enqueue(context.Background(), nil, NewEvent{
		EventType:    eventType,
		EventVersion: version,
		Payload:      json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func newTestOutboxWithBatch(t *testing.T, batch int) (*Outbox, *memStore, func() time.Time) {
	t.Helper()
	store := newMemStore()
	now := func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	p := testPolicy()
	p.BatchSize = batch
	svc, err := New(store, p, now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, now
}

type claimOrderStore struct {
	inner     *memStore
	claimDone atomic.Bool
}

func (c *claimOrderStore) Insert(ctx context.Context, exec Execer, event Event) error {
	return c.inner.Insert(ctx, exec, event)
}

func (c *claimOrderStore) Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Event, error) {
	events, err := c.inner.Claim(ctx, now, lease, limit)
	c.claimDone.Store(true)
	return events, err
}

func (c *claimOrderStore) Complete(ctx context.Context, id ID, now time.Time) error {
	return c.inner.Complete(ctx, id, now)
}

func (c *claimOrderStore) Reschedule(ctx context.Context, id ID, availableAt time.Time, errorClass string) error {
	return c.inner.Reschedule(ctx, id, availableAt, errorClass)
}

func (c *claimOrderStore) IncrementAttempts(ctx context.Context, id ID) (int, error) {
	return c.inner.IncrementAttempts(ctx, id)
}

var _ store = (*claimOrderStore)(nil)
