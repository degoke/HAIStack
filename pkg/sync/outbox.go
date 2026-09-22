package sync

import (
	"context"

	"github.com/degoke/haistack/pkg/store"
)

// Outbox appends resource change events for downstream sync pipelines.
type Outbox interface {
	// Append stores one ResourceEvent and returns it with Sequence when assigned by the backend.
	Append(ctx context.Context, event store.ResourceEvent) (store.ResourceEvent, error)
}

// EventStoreOutbox adapts a store.EventStore to the Outbox interface.
// When used with pkg/core, Append is routed through sync.WithWriteSession for transactional writes.
type EventStoreOutbox struct {
	Events store.EventStore
}

// Append delegates to the underlying event store.
func (o *EventStoreOutbox) Append(ctx context.Context, event store.ResourceEvent) (store.ResourceEvent, error) {
	if o == nil || o.Events == nil {
		return event, nil
	}
	return o.Events.Append(ctx, event)
}
