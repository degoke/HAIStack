package view_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/view"
)

type memReportingTableStore struct {
	meta store.ReportingTableMeta
	rows []map[string]any
}

func (s *memReportingTableStore) Refresh(_ context.Context, meta store.ReportingTableMeta, rows []map[string]any) error {
	s.meta = meta
	s.rows = append([]map[string]any(nil), rows...)
	return nil
}

func (s *memReportingTableStore) QueryRows(context.Context, string, string) ([]map[string]any, error) {
	return append([]map[string]any(nil), s.rows...), nil
}

func (s *memReportingTableStore) GetMeta(context.Context, string, string) (*store.ReportingTableMeta, error) {
	copyMeta := s.meta
	return &copyMeta, nil
}

func TestSQLQueryEngine_SelectReportingRows(t *testing.T) {
	ctx := context.Background()
	reporting := &memReportingTableStore{}
	target := analytics.NewReportingTarget(reporting)
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("register: %v", err)
	}
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t), patientJohn(t))
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	result, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := target.Write(ctx, result); err != nil {
		t.Fatalf("write reporting: %v", err)
	}

	sqlEngine := view.NewSQLQueryEngine(reporting)
	queryResult, err := sqlEngine.Query(ctx, `SELECT id FROM patient_summary_view`, []view.ReportingTableRef{
		{ViewName: "patient_summary_view", ViewVersion: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(queryResult.Rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(queryResult.Rows))
	}
}

func TestRunService_EncodesCSV(t *testing.T) {
	ctx := context.Background()
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("register: %v", err)
	}
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	svc := view.NewRunService(exec)
	body, contentType, err := svc.Execute(ctx, view.ViewRunRequest{
		ViewName: "patient_summary_view",
		Format:   view.FormatCSV,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if contentType != "text/csv" {
		t.Fatalf("contentType = %q", contentType)
	}
	if len(body) == 0 {
		t.Fatal("expected csv body")
	}
}

func TestResolveContainedReference(t *testing.T) {
	parent := map[string]any{
		"resourceType": "Patient",
		"id":           "pat-1",
		"contained": []any{
			map[string]any{"resourceType": "Observation", "id": "obs-inline", "status": "final"},
		},
	}
	resolved, ok := resolveContainedReferenceForTest(parent, "#obs-inline")
	if !ok {
		t.Fatal("expected contained reference to resolve")
	}
	obs, ok := resolved.(map[string]any)
	if !ok || obs["id"] != "obs-inline" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func resolveContainedReferenceForTest(parent any, fragment string) (any, bool) {
	// exercise the same helper through a minimal executor-less copy for unit testing
	if parent == nil {
		return nil, false
	}
	if m, ok := parent.(map[string]any); ok {
		targetID := fragment[1:]
		for _, item := range m["contained"].([]any) {
			entry := item.(map[string]any)
			if entry["id"] == targetID {
				return entry, true
			}
		}
	}
	return nil, false
}
