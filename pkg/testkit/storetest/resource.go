package storetest

import (
	"context"
	"fmt"
	"sort"
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
	if res == nil {
		return fmt.Errorf("resource envelope is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ResourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; ok {
		return fmt.Errorf("resource already exists: %s", key)
	}
	s.data[key] = cloneEnvelope(res)
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
	return cloneEnvelope(res), nil
}

func (s *ResourceStore) Update(_ context.Context, res *types.ResourceEnvelope) error {
	if res == nil {
		return fmt.Errorf("resource envelope is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ResourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	s.data[key] = cloneEnvelope(res)
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

// Seed creates resources and returns the first insertion error.
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
	keys := make([]string, 0, len(s.data))
	for key := range s.data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, cloneEnvelope(s.data[key]))
	}
	return out
}

func (s *ResourceStore) clone() *ResourceStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &ResourceStore{data: cloneResourceData(s.data), lenient: s.lenient}
}

func (s *ResourceStore) replaceFrom(source *ResourceStore) {
	source.mu.Lock()
	data := cloneResourceData(source.data)
	lenient := source.lenient
	source.mu.Unlock()

	s.mu.Lock()
	s.data = data
	s.lenient = lenient
	s.mu.Unlock()
}

func cloneResourceData(data map[string]*types.ResourceEnvelope) map[string]*types.ResourceEnvelope {
	out := make(map[string]*types.ResourceEnvelope, len(data))
	for key, res := range data {
		out[key] = cloneEnvelope(res)
	}
	return out
}

func cloneEnvelope(res *types.ResourceEnvelope) *types.ResourceEnvelope {
	if res == nil {
		return nil
	}
	copy := *res
	copy.JSON = append([]byte(nil), res.JSON...)
	return &copy
}
