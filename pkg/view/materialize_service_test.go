package view_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestMaterializeService_KickoffAndRun(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	views := newMemMaterializedViewStore()
	reg := view.NewRegistry()
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "patient_summary_materialized",
		"version": "1.0.0",
		"resource": "Patient",
		"metadata": {"materialize": "true", "materializeKey": "id"},
		"select": [{"column": [{"name": "id", "path": "Patient.id"}]}]
	}`)
	if _, err := reg.Register(def, defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources:         resources,
		Engine:            defaultEngine(t),
		Registry:          reg,
		MaterializedViews: views,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	svc, err := view.NewMaterializeService(view.MaterializeServiceConfig{
		Jobs:     view.NewInMemoryMaterializeJobStore(),
		Executor: exec,
	})
	if err != nil {
		t.Fatalf("NewMaterializeService: %v", err)
	}
	job, err := svc.Kickoff(ctx, view.MaterializeRequest{ViewName: "patient_summary_materialized"})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != view.MaterializeComplete {
		t.Fatalf("Status = %q, want complete", job.Status)
	}
	if job.RowCount != 1 {
		t.Fatalf("RowCount = %d, want 1", job.RowCount)
	}
}

func TestParseDefinition_SearchMetadata(t *testing.T) {
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "obs_search",
		"version": "1.0.0",
		"resource": "Observation",
		"metadata": {
			"searchParams": "status=final",
			"searchMode": "index"
		},
		"select": [{"column": [{"name": "id", "path": "Observation.id"}]}]
	}`)
	spec, err := view.ParseDefinition(def, defaultEngine(t))
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if spec.SearchMode != view.SearchModeIndex {
		t.Fatalf("SearchMode = %q, want index", spec.SearchMode)
	}
	if spec.SearchParams.Get("status") != "final" {
		t.Fatalf("SearchParams = %v, want status=final", url.Values(spec.SearchParams))
	}
}
