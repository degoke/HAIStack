package search_test

import (
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/search"
)

func TestDefaultPlannerPlanSearch(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)
	planner := search.NewPlanner()

	plan, err := planner.PlanSearch(reg, "Patient", mustValues(t, map[string]string{"name": "Doe"}))
	if err != nil {
		t.Fatalf("PlanSearch: %v", err)
	}
	if plan.ResourceType != "Patient" {
		t.Fatalf("resource type = %q", plan.ResourceType)
	}
	if len(plan.ParamPlans) != 1 {
		t.Fatalf("param plans = %d", len(plan.ParamPlans))
	}
}

func TestExpandIncludesRequiresAdvancedBackend(t *testing.T) {
	backend := &memSearchBackend{}
	executor := search.NewStoreExecutor(backend, newMemResourceStore())
	plan := &search.Plan{
		ResourceType: "Observation",
		Includes: []search.IncludePlan{{
			SourceType:  "Observation",
			ParamCode:   "subject",
			RefFieldKey: "reference.subject",
			TargetType:  "Patient",
		}},
	}
	_, err := executor.Execute(t.Context(), plan)
	if err == nil {
		t.Fatal("expected include error on basic backend")
	}
}

func TestIndexerEmitsFullTextDocument(t *testing.T) {
	ctx := t.Context()
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
		Registry: reg,
		Engine:   testEngine(t),
	})
	if err != nil {
		t.Fatalf("NewRegistryIndexer: %v", err)
	}
	entries, err := indexer.Build(ctx, patientResource(t, "pat-1", "UniqueFullTextName", "555"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	values := fieldValues(entries)
	found := false
	for _, doc := range values["text.document"] {
		if strings.Contains(doc, "UniqueFullTextName") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing full text document: %#v", values)
	}
}
