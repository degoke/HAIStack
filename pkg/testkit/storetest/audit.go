package storetest

import (
	"context"
	"sort"
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
	record = cloneAuditRecord(record)
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
		if query.Action != "" && record.Action != query.Action {
			continue
		}
		if query.Outcome != "" && record.Outcome != query.Outcome {
			continue
		}
		if query.Tenant != "" && record.Tenant != query.Tenant {
			continue
		}
		if query.Subject != "" && record.Subject != query.Subject {
			continue
		}
		if query.ViewName != "" && record.ViewName != query.ViewName {
			continue
		}
		if query.ToolName != "" && record.ToolName != query.ToolName {
			continue
		}
		if query.ConversationID != "" && record.ConversationID != query.ConversationID {
			continue
		}
		if !query.After.IsZero() && record.Timestamp.Before(query.After) {
			continue
		}
		if !query.Before.IsZero() && record.Timestamp.After(query.Before) {
			continue
		}
		out = append(out, cloneAuditRecord(record))
	}
	sortAuditRecords(out)
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return out, nil
}

// Records returns all stored audit records.
func (s *AuditStore) Records() []store.AuditRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.AuditRecord, len(s.records))
	for i, record := range s.records {
		out[i] = cloneAuditRecord(record)
	}
	return out
}

func cloneAuditRecord(record store.AuditRecord) store.AuditRecord {
	copy := record
	if record.Details != nil {
		copy.Details = make(map[string]string, len(record.Details))
		for key, value := range record.Details {
			copy.Details[key] = value
		}
	}
	return copy
}

// sortAuditRecords is shared by tests that need deterministic snapshots.
func sortAuditRecords(records []store.AuditRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		if !records[i].Timestamp.Equal(records[j].Timestamp) {
			return records[i].Timestamp.Before(records[j].Timestamp)
		}
		return records[i].ID < records[j].ID
	})
}
