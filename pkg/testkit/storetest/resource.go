package storetest

import (
	"context"
	"fmt"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

var _ store.ResourceStore = (*ResourceStore)(nil)

// ResourceStore is an in-memory ResourceStore. Use NewResourceStore for strict
// semantics (errors on missing resources) or NewLenientResourceStore for sync
// device nodes that return nil,nil on Read when a resource is absent.
type ResourceStore struct {
	mu      sync.Mutex
	data    map[string]*types.ResourceEnvelope
	lenient bool
}

// NewResourceStore returns a strict in-memory resource store.
func NewResourceStore() *ResourceStore {
	return &ResourceStore{data: make(map[string]*types.ResourceEnvelope)}
}

// NewLenientResourceStore returns an in-memory resource store that returns
// nil,nil from Read when a resource is not found (sync device semantics).
func NewLenientResourceStore() *ResourceStore {
	return &ResourceStore{data: make(map[string]*types.ResourceEnvelope), lenient: true}
}

func (s *ResourceStore) Create(_ context.Context, res *types.ResourceEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ResourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; ok {
		return fmt.Errorf("resource already exists: %s", key)
	}
	s.data[key] = res
	return nil
}

func (s *ResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, ok := s.data[ResourceKey(resourceType, id)]
	if !ok {
		if s.lenient {
			return nil, nil
		}
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	if s.lenient {
		copy := *res
		return &copy, nil
	}
	return res, nil
}

func (s *ResourceStore) Update(_ context.Context, res *types.ResourceEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ResourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	s.data[key] = res
	return nil
}

func (s *ResourceStore) Delete(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ResourceKey(resourceType, id)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	delete(s.data, key)
	return nil
}

func (s *ResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[ResourceKey(resourceType, id)]
	return ok, nil
}

func (s *ResourceStore) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, res := range s.data {
		if res.ResourceType == resourceType {
			ids = append(ids, res.ID)
		}
	}
	if offset >= len(ids) {
		return nil, nil
	}
	end := len(ids)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return ids[offset:end], nil
}

// Seed creates resources, failing the test on error when t is non-nil.
func (s *ResourceStore) Seed(ctx context.Context, resources ...*types.ResourceEnvelope) error {
	for _, res := range resources {
		if err := s.Create(ctx, res); err != nil {
			return err
		}
	}
	return nil
}

// All returns a snapshot of stored resources.
func (s *ResourceStore) All() []*types.ResourceEnvelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*types.ResourceEnvelope, 0, len(s.data))
	for _, res := range s.data {
		out = append(out, res)
	}
	return out
}
