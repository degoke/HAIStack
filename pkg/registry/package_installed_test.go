package registry_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
)

type memPackageInstallStore struct {
	complete map[string]time.Time
}

func (s *memPackageInstallStore) MarkComplete(_ context.Context, packageName, packageVersion string, completedAt time.Time) error {
	if s.complete == nil {
		s.complete = make(map[string]time.Time)
	}
	key := packageName + "@" + packageVersion
	if _, exists := s.complete[key]; exists {
		return nil
	}
	s.complete[key] = completedAt
	return nil
}

func (s *memPackageInstallStore) IsComplete(_ context.Context, packageName, packageVersion string) (bool, error) {
	if s.complete == nil {
		return false, nil
	}
	_, ok := s.complete[packageName+"@"+packageVersion]
	return ok, nil
}

func TestPackageVersionInstalled(t *testing.T) {
	ctx := context.Background()
	packageInstalls := &memPackageInstallStore{}
	mgr := registry.NewManager(registry.Config{
		Definitions:     newMemDefinitionStore(),
		Installs:        newMemInstallStore(),
		PackageInstalls: packageInstalls,
	})
	_ = packageInstalls.MarkComplete(ctx, "test.ig", "2.0.0", time.Now().UTC())
	ok, err := mgr.PackageVersionInstalled(ctx, "test.ig", "2.0.0")
	if err != nil || !ok {
		t.Fatalf("installed=%v err=%v", ok, err)
	}
	ok, err = mgr.PackageVersionInstalled(ctx, "test.ig", "1.0.0")
	if err != nil || ok {
		t.Fatalf("other version installed=%v err=%v", ok, err)
	}
}

func TestMemPackageInstallStorePreservesFirstCompletion(t *testing.T) {
	ctx := context.Background()
	store := &memPackageInstallStore{}
	first := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	second := first.Add(48 * time.Hour)
	_ = store.MarkComplete(ctx, "test.ig", "1.0.0", first)
	_ = store.MarkComplete(ctx, "test.ig", "1.0.0", second)
	if !store.complete["test.ig@1.0.0"].Equal(first) {
		t.Fatalf("completedAt=%v want %v", store.complete["test.ig@1.0.0"], first)
	}
}

func TestPackageVersionInstalledIgnoresPartialCatalogEntries(t *testing.T) {
	ctx := context.Background()
	defs := newMemDefinitionStore()
	mgr := registry.NewManager(registry.Config{
		Definitions:     defs,
		Installs:        newMemInstallStore(),
		PackageInstalls: &memPackageInstallStore{},
	})
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL: "urn:vs", Version: "1", FHIRResourceType: "ValueSet",
		PackageName: "test.ig", PackageVersion: "2.0.0",
	}, nil)
	ok, err := mgr.PackageVersionInstalled(ctx, "test.ig", "2.0.0")
	if err != nil || ok {
		t.Fatalf("partial install should not be complete: installed=%v err=%v", ok, err)
	}
}
