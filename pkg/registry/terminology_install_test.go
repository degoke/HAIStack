package registry_test

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
)

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

func (s *memTerminologyInstallStore) ListInstalled(_ context.Context, filter store.TerminologyInstallFilter) ([]store.TerminologyInstallRecord, error) {
	return store.FilterTerminologyInstalls(s.rows, filter), nil
}

func (s *memTerminologyInstallStore) Delete(_ context.Context, filter store.TerminologyInstallFilter) error {
	next := make([]store.TerminologyInstallRecord, 0, len(s.rows))
	for _, row := range s.rows {
		if filter.CanonicalURL != "" && row.CanonicalURL == filter.CanonicalURL {
			if filter.Version == "" || row.Version == filter.Version {
				if filter.ResourceType == "" || row.ResourceType == filter.ResourceType {
					continue
				}
			}
		}
		next = append(next, row)
	}
	s.rows = next
	return nil
}

func TestDeleteDefinitionRemovesTerminologyInstallRecord(t *testing.T) {
	ctx := context.Background()
	defs := newMemDefinitionStore()
	term := terminology.NewMemoryStore()
	global := terminology.NewMemoryStore()
	installs := &memTerminologyInstallStore{}
	mgr := registry.NewManager(registry.Config{
		Definitions:         defs,
		Installs:            newMemInstallStore(),
		Terminology:         term,
		GlobalTerminology:   global,
		TerminologyScope:    "tenant-a",
		TerminologyInstalls: installs,
		Now:                 func() time.Time { return time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC) },
	})

	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:test-cs","version":"1.0","name":"TestCS","status":"active","concept":[{"code":"a","display":"A"}]}`)
	if err := mgr.InstallDefinition(ctx, cs, registry.InstallProvenance{
		PackageName: "test-pack",
		ModuleName:  "test-mod",
	}); err != nil {
		t.Fatal(err)
	}
	if len(installs.rows) != 1 {
		t.Fatalf("install rows=%d want 1", len(installs.rows))
	}
	if !installs.rows[0].Enabled {
		t.Fatal("registry install should auto-enable terminology for the installing tenant")
	}
	if installs.rows[0].SourceModule != "test-pack" {
		t.Fatalf("source module=%q", installs.rows[0].SourceModule)
	}
	if err := mgr.DeleteDefinition(ctx, "urn:test-cs", "1.0"); err != nil {
		t.Fatal(err)
	}
	if len(installs.rows) != 0 {
		t.Fatalf("install rows after delete=%v", installs.rows)
	}
}

func TestInstallDefinitionRespectsExplicitOptOut(t *testing.T) {
	ctx := context.Background()
	defs := newMemDefinitionStore()
	global := terminology.NewMemoryStore()
	installs := &memTerminologyInstallStore{}
	mgr := registry.NewManager(registry.Config{
		Definitions:         defs,
		Installs:            newMemInstallStore(),
		GlobalTerminology:   global,
		TerminologyInstalls: installs,
		Now:                 func() time.Time { return time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC) },
	})
	cs := []byte(`{"resourceType":"CodeSystem","id":"cs1","url":"urn:shared","version":"1.0","concept":[{"code":"a"}]}`)
	provenance := registry.InstallProvenance{PackageName: "pack-a", PackageVersion: "1.0", SourceModule: "pack-a"}
	if err := mgr.InstallDefinition(ctx, cs, provenance); err != nil {
		t.Fatal(err)
	}
	installs.rows[0].Enabled = false
	if err := mgr.InstallDefinition(ctx, cs, provenance); err != nil {
		t.Fatal(err)
	}
	if installs.rows[0].Enabled {
		t.Fatal("re-install should preserve explicit opt-out")
	}
}
