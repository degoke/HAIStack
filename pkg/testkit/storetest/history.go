package storetest

import (
	"context"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

var _ store.HistoryStore = (*HistoryStore)(nil)

// HistoryStore is an in-memory HistoryStore.
type HistoryStore struct {
	mu   sync.Mutex
	data map[string][]store.ResourceVersion
}

// NewHistoryStore returns an empty in-memory history store.
func NewHistoryStore() *HistoryStore {
	return &HistoryStore{data: make(map[string][]store.ResourceVersion)}
}

func (s *HistoryStore) AppendVersion(_ context.Context, version store.ResourceVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ResourceKey(version.ResourceType, version.ID)
	s.data[key] = append(s.data[key], version)
	return nil
}

func (s *HistoryStore) GetHistory(_ context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.data[ResourceKey(resourceType, id)]
	out := make([]store.ResourceVersion, len(history))
	copy(out, history)
	return out, nil
}
