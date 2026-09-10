package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// NewEvent is the producer input for enqueue. ID is minted by the outbox writer.
type NewEvent struct {
	EventType      string
	EventVersion   int
	AggregateType  string
	AggregateID    string
	Payload        json.RawMessage
	IdempotencyKey string
	CorrelationID  string
}

// Outbox is the platform transactional outbox writer and claim API.
// It does not dispatch to providers. Claim returns after the claim statement
// commits so callers must not hold a DB lock across network I/O.
type Outbox struct {
	store  store
	policy Policy
	now    func() time.Time
}

func New(store store, policy Policy, now func() time.Time) (*Outbox, error) {
	if store == nil {
		return nil, ErrStoreRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Outbox{store: store, policy: policy, now: now}, nil
}

func (o *Outbox) Enqueue(ctx context.Context, exec Execer, in NewEvent) (Event, error) {
	id, err := NewID()
	if err != nil {
		return Event{}, ErrUnavailable
	}
	now := o.now()
	e := Event{
		ID:           id,
		EventType:    strings.TrimSpace(in.EventType),
		EventVersion: in.EventVersion,
		Payload:      append(json.RawMessage(nil), in.Payload...),
		CreatedAt:    now,
		AvailableAt:  now,
		Attempts:     0,
	}
	if in.AggregateType != "" {
		v := in.AggregateType
		e.AggregateType = &v
	}
	if in.AggregateID != "" {
		v := in.AggregateID
		e.AggregateID = &v
	}
	if in.IdempotencyKey != "" {
		v := in.IdempotencyKey
		e.IdempotencyKey = &v
	}
	if in.CorrelationID != "" {
		v := in.CorrelationID
		e.CorrelationID = &v
	}
	if err := e.Validate(); err != nil {
		return Event{}, err
	}
	if err := o.store.Insert(ctx, exec, e); err != nil {
		return Event{}, mapStoreErr(err)
	}
	return e, nil
}

func (o *Outbox) Claim(ctx context.Context, now time.Time) ([]Event, error) {
	if now.IsZero() {
		now = o.now()
	}
	events, err := o.store.Claim(ctx, now, o.policy.Lease, o.policy.BatchSize)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return events, nil
}

func (o *Outbox) Complete(ctx context.Context, id ID, now time.Time) error {
	if id.IsZero() {
		return ErrInvalidEvent
	}
	if now.IsZero() {
		now = o.now()
	}
	return mapStoreErr(o.store.Complete(ctx, id, now))
}

// Reschedule releases a claimed event and sets the next available_at from injected backoff.
// Attempts were incremented at claim time; this does not increment again.
// Dead-letter / max-attempt policy is OPEN and is not applied.
func (o *Outbox) Reschedule(ctx context.Context, event Event, errorClass string, now time.Time) error {
	if event.ID.IsZero() {
		return ErrInvalidEvent
	}
	if strings.TrimSpace(errorClass) == "" {
		return ErrInvalidEvent
	}
	if now.IsZero() {
		now = o.now()
	}
	next, err := o.policy.NextAvailableAt(now, event.Attempts)
	if err != nil {
		return ErrUnavailable
	}
	return mapStoreErr(o.store.Reschedule(ctx, event.ID, next, strings.TrimSpace(errorClass)))
}

func (o *Outbox) IncrementAttempts(ctx context.Context, id ID) (int, error) {
	if id.IsZero() {
		return 0, ErrInvalidEvent
	}
	n, err := o.store.IncrementAttempts(ctx, id)
	if err != nil {
		return 0, mapStoreErr(err)
	}
	return n, nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrInvalidEvent) ||
		errors.Is(err, ErrSensitivePayload) || errors.Is(err, ErrConflict) ||
		errors.Is(err, ErrNotFound) || errors.Is(err, ErrStoreRequired) ||
		errors.Is(err, ErrInvalidPolicy) || errors.Is(err, ErrHandlerRequired) ||
		errors.Is(err, ErrDuplicateHandler) || errors.Is(err, ErrRelayRequired) {
		return err
	}
	return ErrUnavailable
}
