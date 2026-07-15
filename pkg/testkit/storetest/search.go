package storetest

import (
	"context"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

var _ store.SearchStore = (*SearchStore)(nil)

// SearchStore is an in-memory SearchStore with basic field lookup.
type SearchStore struct {
	mu      sync.Mutex
	entries map[string]store.SearchIndexEntry
	lookups map[string][]string
}

// NewSearchStore returns an empty in-memory search store.
func NewSearchStore() *SearchStore {
	return &SearchStore{
		entries: make(map[string]store.SearchIndexEntry),
		lookups: make(map[string][]string),
	}
}

func (s *SearchStore) Index(_ context.Context, entry store.SearchIndexEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ResourceKey(entry.ResourceType, entry.ID)
	s.entries[key] = entry
	for field, value := range entry.Fields {
		lookupKey := field + "=" + value
		s.lookups[lookupKey] = appendUnique(s.lookups[lookupKey], entry.ID)
	}
	return nil
}

func (s *SearchStore) RemoveIndex(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, ResourceKey(resourceType, id))
	return nil
}

func (s *SearchStore) Lookup(_ context.Context, key, value string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.lookups[key+"="+value]
	out := make([]string, len(ids))
	copy(out, ids)
	return out, nil
}

func (s *SearchStore) QueryPrepared(_ context.Context, query store.PreparedQuery, args map[string]string) ([]string, error) {
	if query.Name == "by-field" {
		return s.Lookup(context.Background(), args["key"], args["value"])
	}
	return nil, nil
}

func appendUnique(ids []string, id string) []string {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}
