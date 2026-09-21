package search_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type memSearchBackend struct {
	entries []store.SearchIndexEntry
}

func (m *memSearchBackend) Index(_ context.Context, entry store.SearchIndexEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *memSearchBackend) RemoveIndex(_ context.Context, resourceType, id string) error {
	var kept []store.SearchIndexEntry
	for _, e := range m.entries {
		if e.ResourceType == resourceType && e.ID == id {
			continue
		}
		kept = append(kept, e)
	}
	m.entries = kept
	return nil
}

func (m *memSearchBackend) Lookup(_ context.Context, key, value string) ([]string, error) {
	return m.LookupMatch(context.Background(), store.SearchMatch{FieldKey: key, Value: value})
}

func (m *memSearchBackend) QueryPrepared(context.Context, store.PreparedQuery, map[string]string) ([]string, error) {
	return nil, nil
}

func (m *memSearchBackend) LookupMatch(_ context.Context, match store.SearchMatch) ([]string, error) {
	op := match.Operator
	if op == "" {
		op = "eq"
	}
	if !memOperatorSupported(op) {
		return nil, fmt.Errorf("%w: mem LookupMatch operator %q", store.ErrUnsupportedFeature, op)
	}
	seen := make(map[string]struct{})
	var ids []string
	for _, entry := range m.entries {
		if match.ResourceType != "" && entry.ResourceType != match.ResourceType {
			continue
		}
		for key, value := range entry.Fields {
			if key != match.FieldKey {
				continue
			}
			if !memMatchValue(op, value, match.Value) {
				continue
			}
			if _, ok := seen[entry.ID]; ok {
				continue
			}
			seen[entry.ID] = struct{}{}
			ids = append(ids, entry.ID)
		}
	}
	return ids, nil
}

func memOperatorSupported(op string) bool {
	switch op {
	case "", "eq", "=", "exact", "below", "above", "contains":
		return true
	default:
		return false
	}
}

func memMatchValue(op, have, want string) bool {
	switch op {
	case "", "eq", "=":
		return have == want
	case "below":
		// Hierarchical uri:below: exact self or a child path (prefix + '/').
		// "http://example.org/fhirExtra" is not below "http://example.org/fhir".
		return have == want || strings.HasPrefix(have, want+"/")
	case "above":
		// Hierarchical uri:above: exact self or the query URI is a child of value.
		return want == have || strings.HasPrefix(want, have+"/")
	case "contains":
		return strings.Contains(strings.ToLower(have), strings.ToLower(want))
	case "exact":
		return have == want
	default:
		return false
	}
}

func (m *memSearchBackend) FieldValues(_ context.Context, resourceType, fieldKey string, resourceIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(resourceIDs))
	for _, entry := range m.entries {
		if resourceType != "" && entry.ResourceType != resourceType {
			continue
		}
		for key, value := range entry.Fields {
			if key != fieldKey {
				continue
			}
			for _, id := range resourceIDs {
				if entry.ID == id {
					if _, ok := out[id]; !ok {
						out[id] = value
					}
				}
			}
		}
	}
	return out, nil
}

type memAdvancedSearchBackend struct {
	memSearchBackend
}

func (m *memAdvancedSearchBackend) LookupReferences(_ context.Context, resourceType, fieldKey string, sourceIDs []string) (map[string][]store.ReferenceLink, error) {
	wanted := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		wanted[id] = struct{}{}
	}
	out := make(map[string][]store.ReferenceLink)
	for _, entry := range m.entries {
		if resourceType != "" && entry.ResourceType != resourceType {
			continue
		}
		if _, ok := wanted[entry.ID]; !ok {
			continue
		}
		for key, value := range entry.Fields {
			if key != fieldKey {
				continue
			}
			link := store.ReferenceLink{Literal: value}
			if i := strings.Index(value, "/"); i > 0 && i < len(value)-1 {
				link.TargetType = value[:i]
				link.TargetID = value[i+1:]
			} else if i := strings.Index(value, "|"); i > 0 && i < len(value)-1 {
				link.TargetType = value[:i]
				link.TargetID = value[i+1:]
			} else {
				link.TargetID = value
			}
			out[entry.ID] = append(out[entry.ID], link)
		}
	}
	return out, nil
}

func (m *memAdvancedSearchBackend) LookupReferencing(_ context.Context, sourceType, fieldKey, targetType, targetID string) ([]string, error) {
	values := []string{targetID}
	if targetType != "" {
		values = append(values, targetType+"/"+targetID, targetType+"|"+targetID)
	}
	wanted := make(map[string]struct{}, len(values))
	for _, v := range values {
		wanted[v] = struct{}{}
	}
	seen := make(map[string]struct{})
	var ids []string
	for _, entry := range m.entries {
		if sourceType != "" && entry.ResourceType != sourceType {
			continue
		}
		for key, value := range entry.Fields {
			if key != fieldKey {
				continue
			}
			if _, ok := wanted[value]; !ok {
				continue
			}
			if _, ok := seen[entry.ID]; ok {
				continue
			}
			seen[entry.ID] = struct{}{}
			ids = append(ids, entry.ID)
		}
	}
	return ids, nil
}

func (m *memAdvancedSearchBackend) LookupFullText(context.Context, string, string) (store.FullTextMatch, error) {
	return store.FullTextMatch{}, nil
}

type memResourceStore struct {
	byKey map[string]*types.ResourceEnvelope
}

func newMemResourceStore() *memResourceStore {
	return &memResourceStore{byKey: make(map[string]*types.ResourceEnvelope)}
}

func (m *memResourceStore) key(resourceType, id string) string { return resourceType + "/" + id }

func (m *memResourceStore) Create(_ context.Context, res *types.ResourceEnvelope) error {
	m.byKey[m.key(res.ResourceType, res.ID)] = res
	return nil
}

func (m *memResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	res, ok := m.byKey[m.key(resourceType, id)]
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s", store.ErrNotFound, resourceType, id)
	}
	return res, nil
}

func (m *memResourceStore) Update(_ context.Context, res *types.ResourceEnvelope) error {
	m.byKey[m.key(res.ResourceType, res.ID)] = res
	return nil
}

func (m *memResourceStore) Delete(_ context.Context, resourceType, id string) error {
	delete(m.byKey, m.key(resourceType, id))
	return nil
}

func (m *memResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	_, ok := m.byKey[m.key(resourceType, id)]
	return ok, nil
}

func (m *memResourceStore) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
	var ids []string
	for _, res := range m.byKey {
		if res.ResourceType != resourceType {
			continue
		}
		ids = append(ids, res.ID)
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

func TestReindexWorkerRebuildsRows(t *testing.T) {
	ctx := context.Background()
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
		Registry: reg,
		Engine:   testEngine(t),
	})
	if err != nil {
		t.Fatalf("NewRegistryIndexer: %v", err)
	}

	searchStore := &memSearchBackend{}
	resources := newMemResourceStore()
	env := patientResource(t, "pat-1", "Doe", "555")
	if err := resources.Create(ctx, env); err != nil {
		t.Fatalf("Create: %v", err)
	}

	worker := &search.ReindexWorker{
		Registry:  reg,
		Indexer:   indexer,
		Resources: resources,
		Search:    searchStore,
	}
	if err := worker.ReindexAll(ctx, "Patient"); err != nil {
		t.Fatalf("ReindexAll: %v", err)
	}
	if len(searchStore.entries) == 0 {
		t.Fatal("expected reindexed entries")
	}

	executor := search.NewStoreExecutor(searchStore, resources)
	svc, err := search.NewService(search.ServiceConfig{
		Registry:  reg,
		Executor:  executor,
		Resources: resources,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	result, err := svc.Search(ctx, "Patient", mustValues(t, map[string]string{"name": "Doe"}))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Resources) != 1 || result.Resources[0].ID != "pat-1" {
		t.Fatalf("search result = %#v", result.Resources)
	}
}

func TestMemLookupMatchRejectsUnknownOperator(t *testing.T) {
	backend := &memSearchBackend{entries: []store.SearchIndexEntry{
		{ResourceType: "Patient", ID: "p1", Fields: map[string]string{"string.name": "Jane"}},
	}}
	_, err := backend.LookupMatch(context.Background(), store.SearchMatch{
		ResourceType: "Patient",
		FieldKey:     "string.name",
		Value:        "Jane",
		Operator:     "gt",
	})
	if !errors.Is(err, store.ErrUnsupportedFeature) {
		t.Fatalf("err = %v, want store.ErrUnsupportedFeature", err)
	}
}
