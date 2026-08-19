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
	version.Resource = cloneEnvelope(version.Resource)
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
	for i := range out {
		out[i].Resource = cloneEnvelope(out[i].Resource)
	}
	return out, nil
}

func (s *HistoryStore) clone() *HistoryStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &HistoryStore{data: cloneHistoryData(s.data)}
}

func (s *HistoryStore) replaceFrom(source *HistoryStore) {
	source.mu.Lock()
	data := cloneHistoryData(source.data)
	source.mu.Unlock()
	s.mu.Lock()
	s.data = data
	s.mu.Unlock()
}

func cloneHistoryData(data map[string][]store.ResourceVersion) map[string][]store.ResourceVersion {
	out := make(map[string][]store.ResourceVersion, len(data))
	for key, versions := range data {
		copyVersions := make([]store.ResourceVersion, len(versions))
		copy(copyVersions, versions)
		for i := range copyVersions {
			copyVersions[i].Resource = cloneEnvelope(copyVersions[i].Resource)
		}
		out[key] = copyVersions
	}
	return out
}
