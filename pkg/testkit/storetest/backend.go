package storetest

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
)

var _ store.WriteSessionProvider = (*WriteSessionProvider)(nil)

// Backend bundles in-memory stores for a coherent fake device or hub node.
type Backend struct {
	Resources *ResourceStore
	History   *HistoryStore
	Events    *EventStore
	Cursors   *CursorStore
	Inbox     *InboxStore
	Conflicts *ConflictStore
	Search    *SearchStore
	Audit     *AuditStore
	Jobs      *jobs.InMemoryJobStore
}

// NewDeviceBackend returns a backend configured for sync device nodes (lenient reads).
func NewDeviceBackend() *Backend {
	return &Backend{
		Resources: NewLenientResourceStore(),
		History:   NewHistoryStore(),
		Events:    NewEventStore(),
		Cursors:   NewCursorStore(),
		Inbox:     NewInboxStore(),
		Conflicts: NewConflictStore(),
		Search:    NewSearchStore(),
		Audit:     NewAuditStore(),
		Jobs:      jobs.NewInMemoryJobStore(),
	}
}

// NewStrictBackend returns a backend with strict resource store semantics.
func NewStrictBackend() *Backend {
	b := NewDeviceBackend()
	b.Resources = NewResourceStore()
	return b
}

// WriteSessionProvider provides atomic write sessions over resource, history, search, and events.
type WriteSessionProvider struct {
	Resources *ResourceStore
	History   *HistoryStore
	Search    *SearchStore
	Events    *EventStore
}

// NewWriteSessionProvider returns a write session provider with fresh stores.
func NewWriteSessionProvider() *WriteSessionProvider {
	return &WriteSessionProvider{
		Resources: NewResourceStore(),
		History:   NewHistoryStore(),
		Search:    NewSearchStore(),
		Events:    NewEventStore(),
	}
}

func (p *WriteSessionProvider) BeginWrite(_ context.Context) (store.WriteSession, error) {
	return &writeSession{
		resources: p.Resources,
		history:   p.History,
		search:    p.Search,
		events:    p.Events,
	}, nil
}

type writeSession struct {
	resources *ResourceStore
	history   *HistoryStore
	search    *SearchStore
	events    *EventStore
}

func (s *writeSession) ResourceStore() store.ResourceStore { return s.resources }
func (s *writeSession) HistoryStore() store.HistoryStore   { return s.history }
func (s *writeSession) SearchStore() store.SearchStore     { return s.search }
func (s *writeSession) EventStore() store.EventStore       { return s.events }
func (s *writeSession) Commit(context.Context) error       { return nil }
func (s *writeSession) Rollback(context.Context) error     { return nil }
