package packages_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/packages"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestInstallViewDefinitionRegistersInViewRegistry(t *testing.T) {
	dir := t.TempDir()
	packageDir := filepath.Join(dir, "package")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	viewJSON := []byte(`{
		"resourceType":"ViewDefinition",
		"url":"http://example.org/fhir/ViewDefinition/patient_summary_view",
		"version":"1.0.0",
		"name":"patient_summary_view",
		"status":"active",
		"resource":"Patient",
		"select":[{"column":[{"name":"patient_id","path":"Patient.id","type":"string"}]}]
	}`)
	if err := os.WriteFile(filepath.Join(packageDir, "patient_summary_view.json"), viewJSON, 0o644); err != nil {
		t.Fatal(err)
	}

	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	viewReg := view.NewRegistry()
	installer := &packages.Installer{
		Registry:     testRegistryManager(t),
		ViewRegistry: viewReg,
		FHIRPath:     engine,
	}
	result, err := installer.InstallFromDirectory(context.Background(), packageDir)
	if err != nil {
		t.Fatalf("InstallFromDirectory: %v", err)
	}
	if result.Installed != 1 {
		t.Fatalf("installed=%d want 1", result.Installed)
	}
	if _, err := viewReg.Resolve("patient_summary_view", ""); err != nil {
		t.Fatalf("view registry: %v", err)
	}
}
