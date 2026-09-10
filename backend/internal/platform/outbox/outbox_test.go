package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnqueueValidation(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestOutbox(t)

	if _, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "", EventVersion: 1, Payload: json.RawMessage(`{}`)}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("empty type: %v", err)
	}
	if _, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "notify.x", EventVersion: 0, Payload: json.RawMessage(`{}`)}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("version: %v", err)
	}
	if _, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "notify.x", EventVersion: 1, Payload: json.RawMessage(`null`)}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("null payload: %v", err)
	}
	if _, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "notify.x", EventVersion: 1, Payload: json.RawMessage(`"plain"`)}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("string payload: %v", err)
	}
	if _, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "notify.x", EventVersion: 1, Payload: json.RawMessage(`not-json`)}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid json: %v", err)
	}

	got, err := svc.Enqueue(ctx, nil, NewEvent{
		EventType:      " identity.audit ",
		EventVersion:   1,
		AggregateType:  "session",
		AggregateID:    "agg-1",
		Payload:        json.RawMessage(`{"action":"login","session_id":"opaque"}`),
		IdempotencyKey: "audit:login:1",
		CorrelationID:  "corr-1",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if got.ID.IsZero() || got.EventType != "identity.audit" || got.Attempts != 0 || got.CompletedAt != nil {
		t.Fatalf("event mismatch: %+v", got)
	}
	if !json.Valid(got.Payload) {
		t.Fatal("payload must be structured JSON")
	}
}

func TestSensitivePayloadRejected(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestOutbox(t)
	bad := []string{
		`{"password":"secret"}`,
		`{"nested":{"otp":"123456"}}`,
		`{"access_token":"abc"}`,
		`{"raw_token":"xyz"}`,
		`[{"cookie":"a=b"}]`,
	}
	for _, payload := range bad {
		if _, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "t", EventVersion: 1, Payload: json.RawMessage(payload)}); !errors.Is(err, ErrSensitivePayload) {
			t.Fatalf("%s err = %v, want %v", payload, err, ErrSensitivePayload)
		}
	}
	if len(store.rows) != 0 {
		t.Fatal("sensitive payloads must not be persisted")
	}
	if _, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "t", EventVersion: 1, Payload: json.RawMessage(`{"password_hash":"argon2id"}`)}); err != nil {
		t.Fatalf("non-plaintext hash key should be allowed: %v", err)
	}
}

func TestEnqueueUsesCallerExecutor(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestOutbox(t)
	exec := &recordingExecer{}
	if _, err := svc.Enqueue(ctx, exec, NewEvent{EventType: "t", EventVersion: 1, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if exec.calls != 1 {
		t.Fatalf("exec calls = %d, want 1 (caller transaction)", exec.calls)
	}
}

func TestClaimSemanticsAndLease(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestOutbox(t)
	a, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "a", EventVersion: 1, Payload: json.RawMessage(`{"n":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "b", EventVersion: 1, Payload: json.RawMessage(`{"n":2}`)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "c", EventVersion: 1, Payload: json.RawMessage(`{"n":3}`)})
	if err != nil {
		t.Fatal(err)
	}
	store.setAvailable(c.ID, now().Add(time.Hour))

	first, err := svc.Claim(ctx, now())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("batch size 2: got %d", len(first))
	}
	gotIDs := map[ID]bool{first[0].ID: true, first[1].ID: true}
	if !gotIDs[a.ID] || !gotIDs[b.ID] || gotIDs[c.ID] {
		t.Fatalf("claimed %v, want a and b only", ids(first))
	}
	for _, e := range first {
		if e.Attempts != 1 || e.ClaimedAt == nil || e.ClaimUntil == nil {
			t.Fatalf("lease not applied: %+v", e)
		}
		if !e.ClaimUntil.Equal(now().Add(30 * time.Second)) {
			t.Fatalf("lease duration must come from policy, claim_until=%v", e.ClaimUntil)
		}
	}

	again, err := svc.Claim(ctx, now())
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("active lease must skip rows, got %d", len(again))
	}

	reclaimed, err := svc.Claim(ctx, now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(reclaimed) != 2 {
		t.Fatalf("expired lease reclaim got %d", len(reclaimed))
	}
	if reclaimed[0].Attempts != 2 || reclaimed[1].Attempts != 2 {
		t.Fatalf("reclaim must increment attempts, got %d %d", reclaimed[0].Attempts, reclaimed[1].Attempts)
	}
}

func TestComplete(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestOutbox(t)
	e, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "t", EventVersion: 1, Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.Claim(ctx, now())
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v n=%d", err, len(claimed))
	}
	if err := svc.Complete(ctx, claimed[0].ID, now()); err != nil {
		t.Fatal(err)
	}
	row := store.must(t, e.ID)
	if row.CompletedAt == nil {
		t.Fatal("completed_at must be set")
	}
	if _, err := svc.Claim(ctx, now()); err != nil {
		t.Fatal(err)
	}
	more, _ := svc.Claim(ctx, now().Add(time.Hour))
	if len(more) != 0 {
		t.Fatal("completed events must not be claimed")
	}
	if err := svc.Complete(ctx, e.ID, now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second complete err = %v, want %v", err, ErrNotFound)
	}
}

func TestRescheduleFailureBackoff(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestOutbox(t)
	_, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "t", EventVersion: 1, Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.Claim(ctx, now())
	if err != nil || len(claimed) != 1 {
		t.Fatal(err)
	}
	if err := svc.Reschedule(ctx, claimed[0], "provider_timeout", now()); err != nil {
		t.Fatal(err)
	}
	row := store.must(t, claimed[0].ID)
	if row.ClaimedAt != nil || row.ClaimUntil != nil {
		t.Fatal("failure must release the lease")
	}
	if row.LastErrorClass == nil || *row.LastErrorClass != "provider_timeout" {
		t.Fatalf("error class = %v", row.LastErrorClass)
	}
	if row.Attempts != 1 {
		t.Fatalf("reschedule must not increment attempts again, got %d", row.Attempts)
	}
	want, err := testPolicy().NextAvailableAt(now(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !row.AvailableAt.Equal(want) {
		t.Fatalf("available_at = %v, want injected backoff %v", row.AvailableAt, want)
	}
	if n, err := svc.Claim(ctx, now()); err != nil || len(n) != 0 {
		t.Fatalf("backoff must delay reclaim, n=%d err=%v", len(n), err)
	}
	later, err := svc.Claim(ctx, want)
	if err != nil || len(later) != 1 {
		t.Fatalf("after backoff claim n=%d err=%v", len(later), err)
	}
}

func TestIncrementAttemptsSafe(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestOutbox(t)
	e, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "t", EventVersion: 1, Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.IncrementAttempts(ctx, e.ID)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, err = svc.IncrementAttempts(ctx, e.ID)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if store.must(t, e.ID).Attempts != 2 {
		t.Fatal("attempts must persist")
	}
	if err := svc.Complete(ctx, e.ID, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IncrementAttempts(ctx, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("completed increment err = %v, want %v", err, ErrNotFound)
	}

	e2, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "c", EventVersion: 1, Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.IncrementAttempts(ctx, e2.ID); err != nil {
				t.Errorf("inc: %v", err)
			}
		}()
	}
	wg.Wait()
	if store.must(t, e2.ID).Attempts != 8 {
		t.Fatalf("concurrent attempts = %d, want 8", store.must(t, e2.ID).Attempts)
	}
}

func TestClaimSkipLockedConcurrent(t *testing.T) {
	ctx := context.Background()
	svc, _, now := newTestOutbox(t)
	if _, err := svc.Enqueue(ctx, nil, NewEvent{EventType: "t", EventVersion: 1, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}

	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := svc.Claim(ctx, now())
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			successes.Add(int32(len(got)))
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("SKIP LOCKED successes = %d, want 1", successes.Load())
	}
}

func TestInvalidPolicyAndNilStore(t *testing.T) {
	if _, err := New(nil, testPolicy(), nil); !errors.Is(err, ErrStoreRequired) {
		t.Fatalf("nil store: %v", err)
	}
	if _, err := New(newMemStore(), Policy{}, nil); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("policy: %v", err)
	}
	pg := NewPostgresStore(nil)
	ctx := context.Background()
	if err := pg.Insert(ctx, nil, Event{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil pool insert: %v", err)
	}
	if _, err := pg.Claim(ctx, time.Now(), time.Second, 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil pool claim: %v", err)
	}
}

func TestIdempotencyConflict(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestOutbox(t)
	in := NewEvent{EventType: "t", EventVersion: 1, Payload: json.RawMessage(`{}`), IdempotencyKey: "once"}
	if _, err := svc.Enqueue(ctx, nil, in); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Enqueue(ctx, nil, in); !errors.Is(err, ErrConflict) {
		t.Fatalf("dup err = %v, want %v", err, ErrConflict)
	}
}

func testPolicy() Policy {
	return Policy{
		BatchSize:         2,
		Lease:             30 * time.Second,
		BackoffBase:       time.Minute,
		BackoffMultiplier: 2,
		BackoffCap:        10 * time.Minute,
		Jitter:            0,
	}
}

func newTestOutbox(t *testing.T) (*Outbox, *memStore, func() time.Time) {
	t.Helper()
	store := newMemStore()
	now := func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	svc, err := New(store, testPolicy(), now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, now
}

func ids(events []Event) []ID {
	out := make([]ID, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}

type recordingExecer struct {
	calls int
}

func (r *recordingExecer) Exec(context.Context, string, ...any) (int64, error) {
	r.calls++
	return 1, nil
}

type memStore struct {
	mu   sync.Mutex
	rows map[ID]Event
	keys map[string]ID
}

func newMemStore() *memStore {
	return &memStore{rows: make(map[ID]Event), keys: make(map[string]ID)}
}

func (m *memStore) setAvailable(id ID, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.rows[id]
	e.AvailableAt = at
	m.rows[id] = e
}

func (m *memStore) must(t *testing.T, id ID) Event {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok {
		t.Fatalf("missing %v", id)
	}
	return cloneEvent(e)
}

func (m *memStore) Insert(_ context.Context, exec Execer, event Event) error {
	if exec != nil {
		if _, err := exec.Exec(context.Background(), "INSERT platform.outbox_events"); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if event.IdempotencyKey != nil {
		if _, ok := m.keys[*event.IdempotencyKey]; ok {
			return ErrConflict
		}
		m.keys[*event.IdempotencyKey] = event.ID
	}
	m.rows[event.ID] = cloneEvent(event)
	return nil
}

func (m *memStore) Claim(_ context.Context, now time.Time, lease time.Duration, limit int) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ready []Event
	for _, e := range m.rows {
		if isClaimable(e, now) {
			ready = append(ready, e)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		if !ready[i].AvailableAt.Equal(ready[j].AvailableAt) {
			return ready[i].AvailableAt.Before(ready[j].AvailableAt)
		}
		return strings.Compare(ready[i].ID.String(), ready[j].ID.String()) < 0
	})
	if len(ready) > limit {
		ready = ready[:limit]
	}
	out := make([]Event, 0, len(ready))
	until := now.Add(lease)
	for _, e := range ready {
		e.ClaimedAt = cloneTime(now)
		e.ClaimUntil = cloneTime(until)
		e.Attempts++
		m.rows[e.ID] = e
		out = append(out, cloneEvent(e))
	}
	return out, nil
}

func (m *memStore) Complete(_ context.Context, id ID, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok || e.CompletedAt != nil {
		return ErrNotFound
	}
	e.CompletedAt = cloneTime(now)
	m.rows[id] = e
	return nil
}

func (m *memStore) Reschedule(_ context.Context, id ID, availableAt time.Time, errorClass string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok || e.CompletedAt != nil {
		return ErrNotFound
	}
	e.AvailableAt = availableAt
	e.ClaimedAt = nil
	e.ClaimUntil = nil
	e.LastErrorClass = &errorClass
	m.rows[id] = e
	return nil
}

func (m *memStore) IncrementAttempts(_ context.Context, id ID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok || e.CompletedAt != nil {
		return 0, ErrNotFound
	}
	e.Attempts++
	m.rows[id] = e
	return e.Attempts, nil
}

func cloneEvent(e Event) Event {
	e.Payload = append(json.RawMessage(nil), e.Payload...)
	e.AggregateType = cloneString(e.AggregateType)
	e.AggregateID = cloneString(e.AggregateID)
	e.IdempotencyKey = cloneString(e.IdempotencyKey)
	e.CorrelationID = cloneString(e.CorrelationID)
	e.LastErrorClass = cloneString(e.LastErrorClass)
	if e.ClaimedAt != nil {
		e.ClaimedAt = cloneTime(*e.ClaimedAt)
	}
	if e.ClaimUntil != nil {
		e.ClaimUntil = cloneTime(*e.ClaimUntil)
	}
	if e.CompletedAt != nil {
		e.CompletedAt = cloneTime(*e.CompletedAt)
	}
	return e
}

func cloneString(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func cloneTime(t time.Time) *time.Time {
	v := t
	return &v
}
