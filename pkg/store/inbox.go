package store

import (
	"context"
	"time"
)

// InboxStore tracks applied remote operations for idempotent sync replay.
// Device nodes use it for pull/apply dedupe by canonical event id; hub nodes may use
// it for push dedupe by client-generated event id.
type InboxStore interface {
	MarkApplied(ctx context.Context, id string, appliedAt time.Time) error
	IsApplied(ctx context.Context, id string) (bool, error)
	AppliedAt(ctx context.Context, id string) (*time.Time, error)
}

// PushInboxStore extends InboxStore with durable acknowledgement storage for
// hub-side push idempotency. The payload is an opaque protocol acknowledgement
// so store does not depend on pkg/sync.
type PushInboxStore interface {
	InboxStore
	GetAckPayload(ctx context.Context, id string) ([]byte, bool, error)
	MarkAppliedWithPayload(ctx context.Context, id string, payload []byte, appliedAt time.Time) error
}
