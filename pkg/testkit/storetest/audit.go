package storetest

import (
	"context"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

var _ store.AuditStore = (*AuditStore)(nil)

// AuditStore is an append-only in-memory AuditStore.
type AuditStore struct {
	mu      sync.Mutex
	records []store.AuditRecord
}

// NewAuditStore returns an empty in-memory audit store.
func NewAuditStore() *AuditStore {
	return &AuditStore{}
}

func (s *AuditStore) Append(_ context.Context, record store.AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return nil
}

func (s *AuditStore) List(_ context.Context, query store.AuditQuery) ([]store.AuditRecord, error) {
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
		out = append(out, record)
	}
	return out, nil
}

// Records returns all stored audit records.
func (s *AuditStore) Records() []store.AuditRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.AuditRecord, len(s.records))
	copy(out, s.records)
	return out
}
