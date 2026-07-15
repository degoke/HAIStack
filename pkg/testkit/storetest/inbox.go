package storetest

import (
	"context"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

var _ store.InboxStore = (*InboxStore)(nil)

// InboxStore is an in-memory InboxStore.
type InboxStore struct {
	mu   sync.Mutex
	data map[string]time.Time
}

// NewInboxStore returns an empty in-memory inbox store.
func NewInboxStore() *InboxStore {
	return &InboxStore{data: make(map[string]time.Time)}
}

func (s *InboxStore) MarkApplied(_ context.Context, id string, appliedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = appliedAt
	return nil
}

func (s *InboxStore) IsApplied(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[id]
	return ok, nil
}

func (s *InboxStore) AppliedAt(_ context.Context, id string) (*time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts, ok := s.data[id]
	if !ok {
		return nil, nil
	}
	copy := ts
	return &copy, nil
}
