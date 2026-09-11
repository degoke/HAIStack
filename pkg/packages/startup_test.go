package packages_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/packages"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
)

type memDefinitionStore struct {
	rows []store.DefinitionResourceRecord
}

func (s *memDefinitionStore) Upsert(_ context.Context, record store.DefinitionResourceRecord, _ []store.DefinitionTargetRecord) error {
	s.rows = append(s.rows, record)
	return nil
}

func (s *memDefinitionStore) Get(_ context.Context, canonicalURL, version string) (*store.DefinitionResourceRecord, error) {
	for _, row := range s.rows {
		if row.CanonicalURL == canonicalURL && row.Version == version {
			return &row, nil
		}
	}
	return nil, nil
}

func (s *memDefinitionStore) List(_ context.Context, filter store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	var out []store.DefinitionResourceRecord
	for _, row := range s.rows {
		if filter.PackageName != "" && row.PackageName != filter.PackageName {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *memDefinitionStore) Delete(_ context.Context, _, _ string) error { return nil }

type memInstallStore struct{}

func (s *memInstallStore) SetEnabled(context.Context, store.RegistryInstallRecord) error { return nil }
func (s *memInstallStore) UpsertInstall(context.Context, store.RegistryInstallRecord) error { return nil }
func (s *memInstallStore) ListEnabled(context.Context) ([]store.RegistryInstallRecord, error) {
	return nil, nil
}
func (s *memInstallStore) ListInstalled(context.Context, store.RegistryInstallFilter) ([]store.RegistryInstallRecord, error) {
	return nil, nil
}
func (s *memInstallStore) Delete(context.Context, store.RegistryInstallFilter) error { return nil }

func TestInstallIfNeededSkipsInstalledPackageVersion(t *testing.T) {
	ctx := context.Background()
	defs := &memDefinitionStore{}
	reg := registry.NewManager(registry.Config{
		Definitions: defs,
		Installs:    &memInstallStore{},
	})
	installer := &packages.Installer{Registry: reg}
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL: "urn:vs", Version: "1", FHIRResourceType: "ValueSet",
		PackageName: "local.ig", PackageVersion: "1.0.0",
	}, nil)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "valueset.json"), []byte(`{"resourceType":"ValueSet","id":"vs","url":"urn:new","version":"1","status":"active"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, skipped, err := installer.InstallIfNeeded(ctx, packages.InstallSpec{
		PackageID: "local.ig", Version: "1.0.0", Path: dir,
	})
	if err != nil || !skipped || result.PackageID != "local.ig" {
		t.Fatalf("result=%+v skipped=%v err=%v", result, skipped, err)
	}
	if len(defs.rows) != 1 {
		t.Fatalf("definitions=%d want no second install", len(defs.rows))
	}
}
