package storetest

import (
	"context"
	"fmt"
	"sync"

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
	mu        sync.Mutex
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
	if p == nil || p.Resources == nil || p.History == nil || p.Search == nil || p.Events == nil {
		return nil, fmt.Errorf("write session provider stores are required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return &writeSession{
		provider:  p,
		resources: p.Resources.clone(),
		history:   p.History.clone(),
		search:    p.Search.clone(),
		events:    p.Events.clone(),
	}, nil
}

type writeSession struct {
	provider  *WriteSessionProvider
	resources *ResourceStore
	history   *HistoryStore
	search    *SearchStore
	events    *EventStore
	mu        sync.Mutex
	done      bool
}

func (s *writeSession) ResourceStore() store.ResourceStore { return s.resources }
func (s *writeSession) HistoryStore() store.HistoryStore   { return s.history }
func (s *writeSession) SearchStore() store.SearchStore     { return s.search }
func (s *writeSession) EventStore() store.EventStore       { return s.events }
func (s *writeSession) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return fmt.Errorf("write session is already closed")
	}
	s.provider.mu.Lock()
	defer s.provider.mu.Unlock()
	s.provider.Resources.replaceFrom(s.resources)
	s.provider.History.replaceFrom(s.history)
	s.provider.Search.replaceFrom(s.search)
	s.provider.Events.replaceFrom(s.events)
	s.done = true
	return nil
}

func (s *writeSession) Rollback(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return fmt.Errorf("write session is already closed")
	}
	s.done = true
	return nil
}
