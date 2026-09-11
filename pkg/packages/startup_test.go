package packages_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/packages"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
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

type memTerminologyInstallStore struct {
	rows []store.TerminologyInstallRecord
}

func (s *memTerminologyInstallStore) SetEnabled(_ context.Context, record store.TerminologyInstallRecord) error {
	s.rows = append(s.rows, record)
	return nil
}
func (s *memTerminologyInstallStore) UpsertInstall(ctx context.Context, record store.TerminologyInstallRecord) error {
	return s.SetEnabled(ctx, record)
}
func (s *memTerminologyInstallStore) ListEnabled(_ context.Context) ([]store.TerminologyInstallRecord, error) {
	var out []store.TerminologyInstallRecord
	for _, row := range s.rows {
		if row.Enabled {
			out = append(out, row)
		}
	}
	return out, nil
}
func (s *memTerminologyInstallStore) ListInstalled(_ context.Context, _ store.TerminologyInstallFilter) ([]store.TerminologyInstallRecord, error) {
	return append([]store.TerminologyInstallRecord(nil), s.rows...), nil
}
func (s *memTerminologyInstallStore) Delete(_ context.Context, _ store.TerminologyInstallFilter) error { return nil }

type memPackageInstallStore struct {
	complete map[string]struct{}
}

func (s *memPackageInstallStore) MarkComplete(_ context.Context, packageName, packageVersion string, _ time.Time) error {
	if s.complete == nil {
		s.complete = make(map[string]struct{})
	}
	key := packageName + "@" + packageVersion
	if _, exists := s.complete[key]; exists {
		return nil
	}
	s.complete[key] = struct{}{}
	return nil
}

func (s *memPackageInstallStore) IsComplete(_ context.Context, packageName, packageVersion string) (bool, error) {
	if s.complete == nil {
		return false, nil
	}
	_, ok := s.complete[packageName+"@"+packageVersion]
	return ok, nil
}

func TestInstallIfNeededSkipsInstalledPackageVersion(t *testing.T) {
	ctx := context.Background()
	defs := &memDefinitionStore{}
	packageInstalls := &memPackageInstallStore{}
	reg := registry.NewManager(registry.Config{
		Definitions:     defs,
		Installs:        &memInstallStore{},
		PackageInstalls: packageInstalls,
	})
	installer := &packages.Installer{Registry: reg}
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL: "urn:vs", Version: "1", FHIRResourceType: "ValueSet",
		PackageName: "local.ig", PackageVersion: "1.0.0",
	}, nil)
	_ = packageInstalls.MarkComplete(ctx, "local.ig", "1.0.0", time.Now().UTC())
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

func TestInstallIfNeededOptsInWhenPackageAlreadyInstalled(t *testing.T) {
	ctx := context.Background()
	defs := &memDefinitionStore{}
	global := terminology.NewMemoryStore()
	installs := &memTerminologyInstallStore{}
	packageInstalls := &memPackageInstallStore{}
	mgr := registry.NewManager(registry.Config{
		Definitions:         defs,
		Installs:            &memInstallStore{},
		PackageInstalls:     packageInstalls,
		GlobalTerminology:   global,
		TerminologyInstalls: installs,
	})
	vsJSON := []byte(`{"resourceType":"ValueSet","url":"urn:vs","version":"1","status":"active"}`)
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL: "urn:vs", Version: "1", FHIRResourceType: "ValueSet",
		PackageName: "local.ig", PackageVersion: "1.0.0",
	}, nil)
	_ = global.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: terminology.GlobalScopeID, ResourceType: "ValueSet",
		CanonicalURL: "urn:vs", Version: "1", ResourceJSON: vsJSON,
	})
	_ = packageInstalls.MarkComplete(ctx, "local.ig", "1.0.0", time.Now().UTC())
	installer := &packages.Installer{Registry: mgr}
	_, skipped, err := installer.InstallIfNeeded(ctx, packages.InstallSpec{
		PackageID: "local.ig", Version: "1.0.0", Path: t.TempDir(),
	})
	if err != nil || !skipped {
		t.Fatalf("skipped=%v err=%v", skipped, err)
	}
	if len(installs.rows) != 1 || !installs.rows[0].Enabled {
		t.Fatalf("install rows=%+v", installs.rows)
	}
}
