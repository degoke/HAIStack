package audit

import (
	"context"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// MemoryStore is an optional in-memory store.AuditStore for tests.
type MemoryStore struct {
	mu      sync.Mutex
	records []store.AuditRecord
}

// NewMemoryStore constructs an empty in-memory audit store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

var _ store.AuditStore = (*MemoryStore)(nil)

// Append implements store.AuditStore.
func (s *MemoryStore) Append(_ context.Context, record store.AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return nil
}

// List implements store.AuditStore.
func (s *MemoryStore) List(_ context.Context, query store.AuditQuery) ([]store.AuditRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.AuditRecord
	for _, record := range s.records {
		if query.ResourceType != "" && record.ResourceType != query.ResourceType {
			continue
		}
		if query.ResourceID != "" && record.ResourceID != query.ResourceID {
			continue
		}
		if query.Actor != "" && record.Actor != query.Actor {
			continue
		}
		if !query.After.IsZero() && record.Timestamp.Before(query.After) {
			continue
		}
		if !query.Before.IsZero() && record.Timestamp.After(query.Before) {
			continue
		}
		out = append(out, record)
		if query.Limit > 0 && len(out) >= query.Limit {
			break
		}
	}
	return out, nil
}

// Records returns a copy of all stored records.
func (s *MemoryStore) Records() []store.AuditRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.AuditRecord(nil), s.records...)
}
