package sync

import (
	"context"
	"time"

	"github.com/degoke/health-ai-stack/pkg/conflict"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// ConflictResolutionHandler receives the result of a conflict-processing job so the
// sync runtime can replay or resubmit an auto-merge, or surface review metadata.
type ConflictResolutionHandler interface {
	OnConflictResolution(ctx context.Context, payload ConflictJobPayload, result conflict.MergeResult) error
}

// Hub is the protocol boundary used by Engine to push local events and pull canonical events.
type Hub interface {
	Push(ctx context.Context, events []LocalEvent) ([]PushResult, error)
	Pull(ctx context.Context, afterSequence int64, limit int) ([]CanonicalEvent, error)
}

// PushServer accepts proposed local events from device nodes.
type PushServer interface {
	Push(ctx context.Context, events []LocalEvent) ([]PushResult, error)
}

// PullServer returns canonical events after a sequence checkpoint.
type PullServer interface {
	Pull(ctx context.Context, afterSequence int64, limit int) ([]CanonicalEvent, error)
}

// HubServer combines push and pull handlers for a canonical postgres hub.
type HubServer interface {
	PushServer
	PullServer
}

// ScopedHubServer is the HTTP-safe variant of HubServer. The request's node
// and tenant identity are part of the server operation rather than merely
// metadata carried beside an unscoped call.
type ScopedHubServer interface {
	HubServer
	PushFor(ctx context.Context, nodeID, tenantID string, events []LocalEvent) ([]PushResult, error)
	PullFor(ctx context.Context, nodeID, tenantID string, afterSequence int64, limit int) ([]CanonicalEvent, error)
}

// Clock returns the current time; tests may inject a deterministic clock.
type Clock func() time.Time

// DefaultClock returns UTC now.
func DefaultClock() time.Time {
	return time.Now().UTC()
}

// Cursor names used by the sync engine.
const (
	CursorPush = "sync.push"
	CursorPull = "sync.pull"
)

// Config wires local stores and the hub adapter for one device node.
type Config struct {
	NodeID   string
	TenantID string

	Events    store.EventStore
	Cursors   store.CursorStore
	Inbox     store.InboxStore
	Resources store.ResourceStore
	History   store.HistoryStore
	// Sessions enables atomic pull application. Database-backed sessions should
	// expose store.InboxWriteSession so the inbox mark commits with the apply.
	Sessions  store.WriteSessionProvider
	Conflicts store.ConflictStore
	Jobs      store.JobStore
	Audit     store.AuditStore
	Search    store.SearchStore

	Hub Hub

	// ConflictEngine evaluates persisted conflicts and produces merge/rebase artifacts.
	ConflictEngine *conflict.Engine

	// ConflictResolutionHandler receives the merge/review result so the runtime can replay/resubmit.
	ConflictResolutionHandler ConflictResolutionHandler

	// SearchIndexer optionally builds search rows when applying canonical events locally.
	SearchIndexer SearchIndexer

	Clock Clock

	PushCursorName string
	PullCursorName string
	PushBatchSize  int
	PullBatchSize  int
}

// normalized fills defaults on Config.
func (c Config) normalized() Config {
	if c.Clock == nil {
		c.Clock = DefaultClock
	}
	if c.PushCursorName == "" {
		c.PushCursorName = CursorPush
	}
	if c.PullCursorName == "" {
		c.PullCursorName = CursorPull
	}
	if c.PushBatchSize <= 0 {
		c.PushBatchSize = 100
	}
	if c.PullBatchSize <= 0 {
		c.PullBatchSize = 100
	}
	if c.ConflictEngine == nil {
		c.ConflictEngine = conflict.NewDefaultEngine()
	}
	return c
}
