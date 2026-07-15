package storetest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

var _ store.ConflictStore = (*ConflictStore)(nil)

// ConflictStore is an in-memory ConflictStore with resource indexing.
type ConflictStore struct {
	mu         sync.Mutex
	records    map[string]store.ConflictRecord
	byResource map[string][]string
}

// NewConflictStore returns an empty in-memory conflict store.
func NewConflictStore() *ConflictStore {
	return &ConflictStore{
		records:    make(map[string]store.ConflictRecord),
		byResource: make(map[string][]string),
	}
}

func (s *ConflictStore) Append(_ context.Context, record store.ConflictRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.ID] = record
	key := ResourceKey(record.ResourceType, record.ResourceID)
	s.byResource[key] = append(s.byResource[key], record.ID)
	return nil
}

func (s *ConflictStore) List(_ context.Context, resourceType, resourceID string) ([]store.ConflictRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.byResource[ResourceKey(resourceType, resourceID)]
	out := make([]store.ConflictRecord, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.records[id])
	}
	return out, nil
}

func (s *ConflictStore) Resolve(_ context.Context, id string, resolvedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return fmt.Errorf("conflict not found: %s", id)
	}
	record.ResolvedAt = &resolvedAt
	s.records[id] = record
	return nil
}

// Records returns all stored conflict records.
func (s *ConflictStore) Records() []store.ConflictRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.ConflictRecord, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, record)
	}
	return out
}
