package store

import (
	"context"
	"time"
)

// EventAction describes a local resource change event.
type EventAction string

const (
	EventActionCreate EventAction = "create"
	EventActionUpdate EventAction = "update"
	EventActionDelete EventAction = "delete"
)

// ResourceEvent is an append-only, typed resource change event for replay and sync pipelines.
type ResourceEvent struct {
	Sequence     int64       `json:"sequence"`
	ResourceType string      `json:"resourceType"`
	ID           string      `json:"id"`
	VersionID    string      `json:"versionId"`
	Action       EventAction `json:"action"`
	Timestamp    time.Time   `json:"timestamp"`
	Hash         string      `json:"hash,omitempty"`
}

// EventStore appends local resource change events and reads them from a sequence checkpoint.
// Implementations assign Sequence on append when the caller leaves it unset.
type EventStore interface {
	Append(ctx context.Context, event ResourceEvent) (ResourceEvent, error)
	ReadSince(ctx context.Context, afterSequence int64, limit int) ([]ResourceEvent, error)
	LatestForResource(ctx context.Context, resourceType, id string) (*ResourceEvent, error)
}
