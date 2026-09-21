// Package memstore provides a small in-memory store.ResourceStore for research
// runners. It is not a production persistence backend.
package memstore

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ResourceStore is an in-memory FHIR resource store used by research pipelines.
type ResourceStore struct {
	mu   sync.Mutex
	data map[string]*types.ResourceEnvelope
}

var _ store.ResourceStore = (*ResourceStore)(nil)

// New returns an empty resource store.
func New() *ResourceStore {
	return &ResourceStore{data: make(map[string]*types.ResourceEnvelope)}
}

func resourceKey(resourceType, id string) string {
	return resourceType + "/" + id
}

// Create implements store.ResourceStore.
func (s *ResourceStore) Create(_ context.Context, res *types.ResourceEnvelope) error {
	if res == nil {
		return fmt.Errorf("resource envelope is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; ok {
		return fmt.Errorf("resource already exists: %s", key)
	}
	s.data[key] = clone(res)
	return nil
}

// Read implements store.ResourceStore.
func (s *ResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, ok := s.data[resourceKey(resourceType, id)]
	if !ok {
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	return clone(res), nil
}

// Update implements store.ResourceStore.
func (s *ResourceStore) Update(_ context.Context, res *types.ResourceEnvelope) error {
	if res == nil {
		return fmt.Errorf("resource envelope is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	s.data[key] = clone(res)
	return nil
}

// Delete implements store.ResourceStore.
func (s *ResourceStore) Delete(_ context.Context, resourceType, id string) error {
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
func (s *ResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[resourceKey(resourceType, id)]
	return ok, nil
}

// ListIDs implements store.ResourceStore.
func (s *ResourceStore) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
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
		limit = 1000
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

// All returns a stable snapshot of stored resources.
func (s *ResourceStore) All() []*types.ResourceEnvelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.data))
	for key := range s.data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*types.ResourceEnvelope, 0, len(keys))
	for _, key := range keys {
		out = append(out, clone(s.data[key]))
	}
	return out
}

func clone(res *types.ResourceEnvelope) *types.ResourceEnvelope {
	if res == nil {
		return nil
	}
	cp := *res
	cp.JSON = append([]byte(nil), res.JSON...)
	return &cp
}
