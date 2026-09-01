package packages_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/packages"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestInstallFromDirectorySkipsPackageJSON(t *testing.T) {
	dir := t.TempDir()
	packageDir := filepath.Join(dir, "package")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(`{"name":"demo.ig","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	sd := []byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://example.org/fhir/StructureDefinition/DemoPatient",
		"version":"1.0.0",
		"kind":"resource",
		"type":"Patient",
		"name":"DemoPatient",
		"status":"draft",
		"fhirVersion":"4.0.1"
	}`)
	if err := os.WriteFile(filepath.Join(packageDir, "DemoPatient.json"), sd, 0o644); err != nil {
		t.Fatal(err)
	}

	installer := &packages.Installer{Registry: testRegistryManager(t)}
	result, err := installer.InstallFromDirectory(context.Background(), packageDir)
	if err != nil {
		t.Fatalf("InstallFromDirectory: %v", err)
	}
	if result.Installed != 1 {
		t.Fatalf("installed=%d want 1", result.Installed)
	}
}

func TestInstallFromArchiveSkipsNonFHIRJSON(t *testing.T) {
	archive := buildTestArchive(t, map[string][]byte{
		"package/package.json": []byte(`{"name":"demo.ig","version":"1.0.0"}`),
		"package/DemoPatient.json": []byte(`{
			"resourceType":"StructureDefinition",
			"url":"http://example.org/fhir/StructureDefinition/DemoPatient",
			"version":"1.0.0",
			"kind":"resource",
			"type":"Patient",
			"name":"DemoPatient",
			"status":"draft",
			"fhirVersion":"4.0.1"
		}`),
	})

	installer := &packages.Installer{Registry: testRegistryManager(t)}
	result, err := installer.InstallFromArchive(context.Background(), "demo.ig", "1.0.0", bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("InstallFromArchive: %v", err)
	}
	if result.Installed != 1 {
		t.Fatalf("installed=%d want 1", result.Installed)
	}
}

func testRegistryManager(t *testing.T) *registry.Manager {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "packages-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return registry.NewManager(registry.Config{
		Definitions: db.DefinitionStore(),
		Installs:    db.RegistryInstallStore(),
	})
}

func buildTestArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(data)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
