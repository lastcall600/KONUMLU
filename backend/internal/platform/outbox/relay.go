package outbox

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Relay claims outbox batches, then dispatches handlers after the claim
// statement has completed. Handlers must not run while a DB row lock is held.
type Relay struct {
	outbox   *Outbox
	registry *Registry
	poll     time.Duration
	now      func() time.Time
	wait     func(ctx context.Context, d time.Duration) error
	logf     func(format string, args ...any)
}

func NewRelay(ob *Outbox, registry *Registry, poll time.Duration, now func() time.Time) (*Relay, error) {
	if ob == nil {
		return nil, ErrStoreRequired
	}
	if registry == nil {
		return nil, ErrRelayRequired
	}
	if poll <= 0 {
		return nil, ErrInvalidPolicy
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Relay{
		outbox:   ob,
		registry: registry,
		poll:     poll,
		now:      now,
		wait:     waitPoll,
		logf:     func(string, ...any) {},
	}, nil
}

func (r *Relay) SetLogf(logf func(format string, args ...any)) {
	if r == nil {
		return
	}
	if logf == nil {
		r.logf = func(string, ...any) {}
		return
	}
	r.logf = logf
}

// Run polls until ctx is cancelled. An empty claim waits poll interval.
func (r *Relay) Run(ctx context.Context) error {
	return r.RunWorkers(ctx, 1)
}

// RunWorkers starts n competing poll loops. SKIP LOCKED claim keeps delivery
// semantics; in-flight Handle calls finish or lose the lease and retry.
func (r *Relay) RunWorkers(ctx context.Context, n int) error {
	if r == nil {
		return ErrRelayRequired
	}
	if n <= 0 {
		return ErrInvalidPolicy
	}
	if n == 1 {
		return r.runLoop(ctx)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.runLoop(ctx); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (r *Relay) runLoop(ctx context.Context) error {
	if r == nil {
		return ErrRelayRequired
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		n, err := r.processBatch(ctx)
		if err != nil {
			if canceled(err) {
				return nil
			}
			r.logf("outbox claim failed class=%s", "claim_failed")
		}
		if n > 0 && err == nil {
			continue
		}
		if waitErr := r.wait(ctx, r.poll); waitErr != nil {
			return nil
		}
	}
}

func (r *Relay) processBatch(ctx context.Context) (int, error) {
	events, err := r.outbox.Claim(ctx, r.now())
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		if ctx.Err() != nil {
			return len(events), ctx.Err()
		}
		r.dispatch(ctx, event)
	}
	return len(events), nil
}

func (r *Relay) dispatch(ctx context.Context, event Event) {
	h, ok := r.registry.Lookup(event.EventType, event.EventVersion)
	if !ok {
		r.logf("outbox unknown handler type=%s version=%d id=%s class=%s", event.EventType, event.EventVersion, event.ID.String(), ErrorClassUnknownHandler)
		r.reschedule(ctx, event, ErrorClassUnknownHandler)
		return
	}
	if err := h.Handle(ctx, event); err != nil {
		if canceled(err) {
			return
		}
		r.logf("outbox handler failed type=%s version=%d id=%s class=%s", event.EventType, event.EventVersion, event.ID.String(), ErrorClassHandlerFailed)
		r.reschedule(ctx, event, ErrorClassHandlerFailed)
		return
	}
	if err := r.outbox.Complete(ctx, event.ID, r.now()); err != nil && !canceled(err) {
		r.logf("outbox complete failed type=%s version=%d id=%s class=%s", event.EventType, event.EventVersion, event.ID.String(), "complete_failed")
	}
}

func (r *Relay) reschedule(ctx context.Context, event Event, errorClass string) {
	if err := r.outbox.Reschedule(ctx, event, errorClass, r.now()); err != nil && !canceled(err) {
		r.logf("outbox reschedule failed type=%s version=%d id=%s class=%s", event.EventType, event.EventVersion, event.ID.String(), "reschedule_failed")
	}
}

func waitPoll(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func canceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
