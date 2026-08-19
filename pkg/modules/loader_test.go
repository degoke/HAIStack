package modules_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/modules"
)

func TestLoaderReadsManifestAndDefinitions(t *testing.T) {
	loader := modules.NewLoader()
	mod, err := loader.Load(filepath.Join("..", "..", "modules", "core"))
	if err != nil {
		t.Fatalf("Load core: %v", err)
	}
	if mod.Manifest.Name != "core" {
		t.Errorf("name = %q, want core", mod.Manifest.Name)
	}
	if mod.Manifest.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", mod.Manifest.Version)
	}
	if len(mod.Definitions) != 1 {
		t.Errorf("definitions = %d, want 1", len(mod.Definitions))
	}
}

func TestLoaderMissingManifest(t *testing.T) {
	loader := modules.NewLoader()
	_, err := loader.Load(filepath.Join("..", "..", "modules", "not-a-module"))
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if !isError(err, modules.ErrManifestNotFound) {
		t.Errorf("error = %v, want ErrManifestNotFound", err)
	}
}

func TestLoaderInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "module.json"), []byte(`{"name":"","version":"1.0.0"}`))
	loader := modules.NewLoader()
	_, err := loader.Load(dir)
	if err == nil {
		t.Fatal("expected error for invalid manifest")
	}
	if !isError(err, modules.ErrInvalidManifest) {
		t.Errorf("error = %v, want ErrInvalidManifest", err)
	}
}

func TestLoaderBadSemver(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "module.json"), []byte(`{"name":"x","version":"abc"}`))
	loader := modules.NewLoader()
	_, err := loader.Load(dir)
	if err == nil {
		t.Fatal("expected error for bad semver")
	}
	if !isError(err, modules.ErrInvalidManifest) {
		t.Errorf("error = %v, want ErrInvalidManifest", err)
	}
}

func TestLoaderDuplicateDependency(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "module.json"), []byte(`{
		"name":"x","version":"1.0.0",
		"dependencies":[{"name":"core","version":"1.0.0"},{"name":"core","version":"1.0.0"}]
	}`))
	loader := modules.NewLoader()
	_, err := loader.Load(dir)
	if err == nil {
		t.Fatal("expected error for duplicate dependency")
	}
	if !isError(err, modules.ErrInvalidManifest) {
		t.Errorf("error = %v, want ErrInvalidManifest", err)
	}
}

func TestLoaderDefinitionFileEscapesModuleDir(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "module.json"), []byte(`{
		"name":"x","version":"1.0.0",
		"definitionFiles":["../escape.json"]
	}`))
	loader := modules.NewLoader()
	_, err := loader.Load(dir)
	if err == nil {
		t.Fatal("expected error for escaping definition file")
	}
	if !isError(err, modules.ErrInvalidManifest) {
		t.Errorf("error = %v, want ErrInvalidManifest", err)
	}
}

func TestLoaderRejectsOversizedManifest(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "module.json"), []byte(`{"name":"x","version":"1.0.0","description":"`+strings.Repeat("x", 1<<20)+`"}`))
	_, err := modules.NewLoader().Load(dir)
	if err == nil || !errors.Is(err, modules.ErrModuleFileTooLarge) {
		t.Fatalf("Load oversized manifest error = %v, want ErrModuleFileTooLarge", err)
	}
}

func TestLoaderRejectsDefinitionSymlinkOutsideModule(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "definition.json"), []byte(`{"resourceType":"SearchParameter"}`))
	if err := os.Symlink(filepath.Join(outside, "definition.json"), filepath.Join(dir, "definition.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	mustWriteFile(t, filepath.Join(dir, "module.json"), []byte(`{
		"name":"x","version":"1.0.0","definitionFiles":["definition.json"]
	}`))
	_, err := modules.NewLoader().Load(dir)
	if err == nil || !errors.Is(err, modules.ErrInvalidManifest) {
		t.Fatalf("Load symlinked definition error = %v, want ErrInvalidManifest", err)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func isError(err, target error) bool {
	return errors.Is(err, target)
}
