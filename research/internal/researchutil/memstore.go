package researchutil

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/types"
)

var _ store.ResourceStore = (*MemoryResourceStore)(nil)

// MemoryResourceStore is an in-memory store.ResourceStore for research CLIs.
// Evaluation runners use this instead of pkg/testkit/storetest (tests-only).
type MemoryResourceStore struct {
	mu   sync.Mutex
	data map[string]*types.ResourceEnvelope
}

// NewMemoryResourceStore returns an empty strict in-memory resource store.
func NewMemoryResourceStore() *MemoryResourceStore {
	return &MemoryResourceStore{data: make(map[string]*types.ResourceEnvelope)}
}

func resourceKey(resourceType, id string) string {
	return resourceType + "/" + id
}

func cloneEnvelope(res *types.ResourceEnvelope) *types.ResourceEnvelope {
	if res == nil {
		return nil
	}
	copy := *res
	copy.JSON = append([]byte(nil), res.JSON...)
	return &copy
}

// Create implements store.ResourceStore.
func (s *MemoryResourceStore) Create(_ context.Context, res *types.ResourceEnvelope) error {
	if res == nil {
		return fmt.Errorf("resource envelope is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; ok {
		return fmt.Errorf("resource already exists: %s", key)
	}
	s.data[key] = cloneEnvelope(res)
	return nil
}

// Read implements store.ResourceStore.
func (s *MemoryResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, ok := s.data[resourceKey(resourceType, id)]
	if !ok {
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	return cloneEnvelope(res), nil
}

// Update implements store.ResourceStore.
func (s *MemoryResourceStore) Update(_ context.Context, res *types.ResourceEnvelope) error {
	if res == nil {
		return fmt.Errorf("resource envelope is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	s.data[key] = cloneEnvelope(res)
	return nil
}

// Delete implements store.ResourceStore.
func (s *MemoryResourceStore) Delete(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(resourceType, id)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	delete(s.data, key)
	return nil
}

// Exists implements store.ResourceStore.
func (s *MemoryResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[resourceKey(resourceType, id)]
	return ok, nil
}

// ListIDs implements store.ResourceStore.
func (s *MemoryResourceStore) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
	if offset < 0 {
		return nil, fmt.Errorf("offset must be non-negative")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, res := range s.data {
		if res.ResourceType == resourceType {
			ids = append(ids, res.ID)
		}
	}
	sort.Strings(ids)
	if limit <= 0 {
		limit = 100
	}
	if offset >= len(ids) {
		return nil, nil
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	return append([]string(nil), ids[offset:end]...), nil
}
