package hooks

import (
	"context"
	"fmt"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// Point is one of the four intercept stages.
type Point string

const (
	Incoming   Point = "incoming"
	PreStorage Point = "pre-storage"
	PostCommit Point = "post-commit"
	Outgoing   Point = "outgoing"
)

// Action names the FHIR interaction that triggered the hook.
type Action string

const (
	ActionRead        Action = "read"
	ActionCreate      Action = "create"
	ActionUpdate      Action = "update"
	ActionPatch       Action = "patch"
	ActionDelete      Action = "delete"
	ActionSearch      Action = "search"
	ActionHistory     Action = "history"
	ActionTransaction Action = "transaction"
	ActionOperation   Action = "operation"
	ActionMetadata    Action = "metadata"
)

// Event is the payload delivered to a hook. Resource is mutable on
// pre-storage and outgoing so a hook can replace or annotate it.
type Event struct {
	Point        Point
	Action       Action
	ResourceType string
	ID           string
	Operation    string
	Resource     *types.ResourceEnvelope
	Previous     *types.ResourceEnvelope
}

// Func is one intercept callback.
type Func func(ctx context.Context, event *Event) error

// Hooks runs the four intercept stages. Registry implements it.
type Hooks interface {
	Run(ctx context.Context, point Point, event *Event) error
}

// Registry holds Func values for the four points.
type Registry struct {
	mu    sync.RWMutex
	hooks map[Point][]Func
}

// NewRegistry returns an empty hook registry.
func NewRegistry() *Registry {
	return &Registry{hooks: make(map[Point][]Func)}
}

// On registers fn for point. Unknown points are rejected.
func (r *Registry) On(point Point, fn Func) error {
	if r == nil {
		return fmt.Errorf("hooks: registry is nil")
	}
	if fn == nil {
		return fmt.Errorf("hooks: func is nil")
	}
	switch point {
	case Incoming, PreStorage, PostCommit, Outgoing:
	default:
		return fmt.Errorf("hooks: unknown point %q", point)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hooks == nil {
		r.hooks = make(map[Point][]Func)
	}
	r.hooks[point] = append(r.hooks[point], fn)
	return nil
}

// Run invokes registered funcs for point in registration order.
func (r *Registry) Run(ctx context.Context, point Point, event *Event) error {
	if r == nil {
		return nil
	}
	if event == nil {
		event = &Event{}
	}
	event.Point = point
	r.mu.RLock()
	fns := append([]Func(nil), r.hooks[point]...)
	r.mu.RUnlock()
	for _, fn := range fns {
		if err := fn(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
