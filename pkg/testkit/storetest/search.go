package storetest

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
)

var _ store.SearchStore = (*SearchStore)(nil)
var _ store.SearchQueryExecutor = (*SearchStore)(nil)

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
	if prior, ok := s.entries[key]; ok {
		s.removeLookupEntriesLocked(key, prior)
		fields := cloneFields(prior.Fields)
		for field, value := range entry.Fields {
			fields[field] = value
		}
		entry.Fields = fields
	}
	entry.Fields = cloneFields(entry.Fields)
	s.entries[key] = entry
	for field, value := range entry.Fields {
		lookupKey := field + "=" + value
		s.lookups[lookupKey] = appendUnique(s.lookups[lookupKey], key)
	}
	return nil
}

func (s *SearchStore) RemoveIndex(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ResourceKey(resourceType, id)
	if entry, ok := s.entries[key]; ok {
		s.removeLookupEntriesLocked(key, entry)
		delete(s.entries, key)
	}
	return nil
}

func (s *SearchStore) Lookup(_ context.Context, key, value string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := s.lookups[key+"="+value]
	ids := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, resourceKey := range keys {
		entry, ok := s.entries[resourceKey]
		if !ok {
			continue
		}
		if _, ok := seen[entry.ID]; ok {
			continue
		}
		seen[entry.ID] = struct{}{}
		ids = append(ids, entry.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *SearchStore) QueryPrepared(_ context.Context, query store.PreparedQuery, args map[string]string) ([]string, error) {
	if query.Name == "by-field" {
		return s.Lookup(context.Background(), args["key"], args["value"])
	}
	return nil, fmt.Errorf("unknown prepared search query %q", query.Name)
}

// LookupMatch returns IDs matching one typed field predicate. It implements
// store.SearchQueryExecutor so the same fake can back both search.Service and
// core.ResourceService in an integration harness.
func (s *SearchStore) LookupMatch(_ context.Context, match store.SearchMatch) ([]string, error) {
	if match.FieldKey == "" {
		return nil, fmt.Errorf("search field key is required")
	}
	if match.Operator != "" && match.Operator != "=" && match.Operator != "eq" {
		return nil, fmt.Errorf("unsupported search operator %q", match.Operator)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.entries))
	for key := range s.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		entry := s.entries[key]
		if match.ResourceType != "" && entry.ResourceType != match.ResourceType {
			continue
		}
		if entry.Fields[match.FieldKey] == match.Value {
			ids = append(ids, entry.ID)
		}
	}
	return ids, nil
}

// FieldValues returns indexed values for the requested resource IDs.
func (s *SearchStore) FieldValues(_ context.Context, resourceType, fieldKey string, resourceIDs []string) (map[string]string, error) {
	if fieldKey == "" {
		return nil, fmt.Errorf("search field key is required")
	}
	wanted := make(map[string]struct{}, len(resourceIDs))
	for _, id := range resourceIDs {
		wanted[id] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.entries))
	for key := range s.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string)
	for _, key := range keys {
		entry := s.entries[key]
		if resourceType != "" && entry.ResourceType != resourceType {
			continue
		}
		if _, ok := wanted[entry.ID]; !ok {
			continue
		}
		if value, ok := entry.Fields[fieldKey]; ok {
			out[entry.ID] = value
		}
	}
	return out, nil
}

func (s *SearchStore) clone() *SearchStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &SearchStore{entries: cloneEntries(s.entries), lookups: cloneLookups(s.lookups)}
}

func (s *SearchStore) replaceFrom(source *SearchStore) {
	source.mu.Lock()
	entries := cloneEntries(source.entries)
	lookups := cloneLookups(source.lookups)
	source.mu.Unlock()
	s.mu.Lock()
	s.entries = entries
	s.lookups = lookups
	s.mu.Unlock()
}

func (s *SearchStore) removeLookupEntriesLocked(resourceKey string, entry store.SearchIndexEntry) {
	for field, value := range entry.Fields {
		lookupKey := field + "=" + value
		keys := s.lookups[lookupKey]
		kept := keys[:0]
		for _, key := range keys {
			if key != resourceKey {
				kept = append(kept, key)
			}
		}
		if len(kept) == 0 {
			delete(s.lookups, lookupKey)
		} else {
			s.lookups[lookupKey] = kept
		}
	}
}

func cloneFields(fields map[string]string) map[string]string {
	if fields == nil {
		return nil
	}
	out := make(map[string]string, len(fields))
	for key, value := range fields {
		out[key] = value
	}
	return out
}

func cloneEntries(entries map[string]store.SearchIndexEntry) map[string]store.SearchIndexEntry {
	out := make(map[string]store.SearchIndexEntry, len(entries))
	for key, entry := range entries {
		entry.Fields = cloneFields(entry.Fields)
		out[key] = entry
	}
	return out
}

func cloneLookups(lookups map[string][]string) map[string][]string {
	out := make(map[string][]string, len(lookups))
	for key, values := range lookups {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func appendUnique(ids []string, id string) []string {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}
