package outbox

import (
	"strings"
	"sync"
)

type handlerKey struct {
	eventType    string
	eventVersion int
}

// Registry maps event_type + event_version to a Handler.
// It does not import domain packages; cmd/worker wires implementations.
type Registry struct {
	mu       sync.RWMutex
	handlers map[handlerKey]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[handlerKey]Handler)}
}

func (r *Registry) Register(eventType string, eventVersion int, h Handler) error {
	if r == nil {
		return ErrRelayRequired
	}
	if h == nil {
		return ErrHandlerRequired
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" || eventVersion <= 0 {
		return ErrInvalidEvent
	}
	key := handlerKey{eventType: eventType, eventVersion: eventVersion}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[key]; exists {
		return ErrDuplicateHandler
	}
	r.handlers[key] = h
	return nil
}

func (r *Registry) Lookup(eventType string, eventVersion int) (Handler, bool) {
	if r == nil {
		return nil, false
	}
	key := handlerKey{eventType: strings.TrimSpace(eventType), eventVersion: eventVersion}
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[key]
	return h, ok
}
