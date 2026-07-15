package storetest

import (
	"context"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

var _ store.CursorStore = (*CursorStore)(nil)

// CursorStore is an in-memory CursorStore.
type CursorStore struct {
	mu   sync.Mutex
	data map[string]store.Cursor
}

// NewCursorStore returns an empty in-memory cursor store.
func NewCursorStore() *CursorStore {
	return &CursorStore{data: make(map[string]store.Cursor)}
}

func (s *CursorStore) GetCursor(_ context.Context, name string) (*store.Cursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursor, ok := s.data[name]
	if !ok {
		return nil, nil
	}
	copy := cursor
	return &copy, nil
}

func (s *CursorStore) UpsertCursor(_ context.Context, cursor store.Cursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[cursor.Name] = cursor
	return nil
}

func (s *CursorStore) DeleteCursor(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, name)
	return nil
}
