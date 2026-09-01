package http_test

import (
	"context"
	"path/filepath"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

type stubConformanceRuntime struct {
	snapshot *registry.Snapshot
}

func (s *stubConformanceRuntime) Snapshot() *registry.Snapshot { return s.snapshot }
func (s *stubConformanceRuntime) Engine() validate.Engine      { return nil }
func (s *stubConformanceRuntime) Refresh(context.Context) (*registry.Snapshot, error) {
	return s.snapshot, nil
}

func TestLivePatientSearchParamResolverUsesRuntimeSnapshot(t *testing.T) {
	ctx := context.Background()
	runtime := &stubConformanceRuntime{}
	resolver := hahttp.LivePatientSearchParamResolver{Runtime: runtime}

	if _, ok := resolver.PatientSearchParameterCode("Observation"); ok {
		t.Fatal("expected no scope without snapshot")
	}

	mgr := testRegistryManager(t)
	if err := mgr.SeedBundled(ctx); err != nil {
		t.Fatal(err)
	}
	for _, resourceType := range []string{"Patient", "Observation"} {
		if err := mgr.EnableResource(ctx, resourceType); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := mgr.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runtime.snapshot = snapshot

	code, ok := resolver.PatientSearchParameterCode("Observation")
	if !ok || code != "subject" {
		t.Fatalf("code=%q ok=%v want subject", code, ok)
	}
}

func testRegistryManager(t *testing.T) *registry.Manager {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "http-conformance.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return registry.NewManager(registry.Config{
		Definitions: db.DefinitionStore(),
		Installs:    db.RegistryInstallStore(),
	})
}
