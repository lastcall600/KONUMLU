package outbox

import "context"

// Handler dispatches a claimed outbox event. Implementations are registered
// by event_type + event_version at the worker composition root.
type Handler interface {
	Handle(ctx context.Context, event Event) error
}

// HandlerFunc adapts a function to Handler. No reflection.
type HandlerFunc func(ctx context.Context, event Event) error

func (f HandlerFunc) Handle(ctx context.Context, event Event) error {
	return f(ctx, event)
}
