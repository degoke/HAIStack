package subscriptions

import (
	"context"
	"sync"
)

// LocalHandler processes one matched subscription event in-process.
type LocalHandler func(ctx context.Context, payload DeliverPayload, resourceJSON []byte, metadata map[string]any) error

// HandlerRegistry maps handler names to in-process delivery callbacks.
type HandlerRegistry struct {
	mu       sync.RWMutex
	handlers map[string]LocalHandler
}

// NewHandlerRegistry returns an empty handler registry.
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{handlers: make(map[string]LocalHandler)}
}

// Register adds or replaces a named local handler.
func (r *HandlerRegistry) Register(name string, handler LocalHandler) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers == nil {
		r.handlers = make(map[string]LocalHandler)
	}
	r.handlers[name] = handler
}

// Lookup returns a registered handler by name.
func (r *HandlerRegistry) Lookup(name string) (LocalHandler, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}
