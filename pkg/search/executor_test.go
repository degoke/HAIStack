package search_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestStoreExecutorSortByLastUpdated(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	_ = resources.Create(ctx, &types.ResourceEnvelope{ResourceType: "Patient", ID: "pat-1"})
	_ = resources.Create(ctx, &types.ResourceEnvelope{ResourceType: "Patient", ID: "pat-2"})
	backend := &memSearchBackend{
		entries: []store.SearchIndexEntry{
			{ResourceType: "Patient", ID: "pat-1", Fields: map[string]string{"date._lastUpdated": "2024-01-01T00:00:00Z"}},
			{ResourceType: "Patient", ID: "pat-2", Fields: map[string]string{"date._lastUpdated": "2024-06-01T00:00:00Z"}},
		},
	}
	executor := search.NewStoreExecutor(backend, resources)
	plan := &search.Plan{
		ResourceType: "Patient",
		Count:        10,
		Sort:         []search.SortField{{Code: "_lastUpdated", FieldKey: "date._lastUpdated", Direction: search.SortDesc}},
	}

	result, err := executor.Execute(ctx, plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	ids := result.IDs
	if len(ids) != 2 || ids[0] != "pat-2" || ids[1] != "pat-1" {
		t.Fatalf("sorted ids = %v", ids)
	}
}

func TestStoreExecutorSortByIDAscending(t *testing.T) {
	ctx := context.Background()
	backend := &memSearchBackend{}
	resources := newMemResourceStore()
	for _, id := range []string{"pat-b", "pat-a"} {
		_ = resources.Create(ctx, &types.ResourceEnvelope{ResourceType: "Patient", ID: id})
		backend.entries = append(backend.entries, store.SearchIndexEntry{
			ResourceType: "Patient",
			ID:           id,
			Fields:       map[string]string{"token._id": id},
		})
	}
	executor := search.NewStoreExecutor(backend, resources)
	plan := &search.Plan{
		ResourceType: "Patient",
		Count:        10,
		Sort:         []search.SortField{{Code: "_id", FieldKey: "token._id", Direction: search.SortAsc}},
	}

	result, err := executor.Execute(ctx, plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	ids := result.IDs
	if len(ids) != 2 || ids[0] != "pat-a" || ids[1] != "pat-b" {
		t.Fatalf("sorted ids = %v", ids)
	}
}
