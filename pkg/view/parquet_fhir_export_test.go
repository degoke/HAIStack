package view_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func bundledPatientCatalog(t *testing.T) validate.MemoryProfileCatalog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "registry", "internal", "bundles", "r4", "structure-definitions", "Patient.json"))
	if err != nil {
		t.Fatalf("read Patient StructureDefinition: %v", err)
	}
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{raw})
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromJSON: %v", err)
	}
	return catalog
}

func TestWriteParquetFHIRExportNestedResources(t *testing.T) {
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
		Resources:      resources,
		Engine:         engine,
		Registry:       reg,
		ProfileCatalog: bundledPatientCatalog(t),
	})
	if err != nil {
		t.Fatalf("executor: %v", err)
	}

	var buf bytes.Buffer
	rowCount, _, err := view.WriteParquetFHIRExport(context.Background(), &buf, exec, view.ExecuteRequest{
		ViewName: "patient_summary_view",
		Version:  "1.0.0",
	})
	if err != nil {
		t.Fatalf("WriteParquetFHIRExport: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("rowCount=%d, want 2", rowCount)
	}
	if !view.IsParquetFile(buf.Bytes()) {
		t.Fatal("expected parquet output")
	}
}

func TestWriteParquetFHIRExportRequiresProfileCatalog(t *testing.T) {
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
	var buf bytes.Buffer
	_, _, err = view.WriteParquetFHIRExport(context.Background(), &buf, exec, view.ExecuteRequest{
		ViewName: "patient_summary_view",
	})
	if err == nil {
		t.Fatal("expected error without ProfileCatalog")
	}
}
