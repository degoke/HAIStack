package sync

import (
	"context"

	"github.com/degoke/haistack/pkg/store"
)

// SessionOutbox appends events through a write session's event store.
type SessionOutbox struct {
	Session store.WriteSession
}

// Append delegates to the session-scoped event store.
func (o *SessionOutbox) Append(ctx context.Context, event store.ResourceEvent) (store.ResourceEvent, error) {
	if o == nil || o.Session == nil {
		return event, nil
	}
	return o.Session.EventStore().Append(ctx, event)
}

// WithWriteSession returns an Outbox that routes Append through session for transactional writes.
// EventStoreOutbox instances use the session event store; other Outbox implementations are called directly.
func WithWriteSession(o Outbox, session store.WriteSession) Outbox {
	if o == nil {
		return nil
	}
	return &sessionBoundOutbox{outbox: o, session: session}
}

type sessionBoundOutbox struct {
	outbox  Outbox
	session store.WriteSession
}

func (b *sessionBoundOutbox) Append(ctx context.Context, event store.ResourceEvent) (store.ResourceEvent, error) {
	if b.outbox == nil {
		return event, nil
	}
	if _, ok := b.outbox.(*EventStoreOutbox); ok {
		return (&SessionOutbox{Session: b.session}).Append(ctx, event)
	}
	return b.outbox.Append(ctx, event)
}
