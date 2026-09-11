package terminology

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestEnableCatalogEntryRequiresGlobalResource(t *testing.T) {
	ctx := context.Background()
	global := NewMemoryStore()
	installs := &memTerminologyInstallStore{}
	_, err := EnableCatalogEntry(ctx, CatalogEnableOptions{Global: global, Installs: installs}, store.TerminologyInstallRecord{
		CanonicalURL: "urn:missing", Version: "1", ResourceType: "ValueSet", Enabled: true,
	})
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestEnableCatalogEntryEnablesExistingGlobalResource(t *testing.T) {
	ctx := context.Background()
	global := NewMemoryStore()
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:cs","version":"1","concept":[{"code":"a"}]}`)
	_ = global.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:cs", Version: "1", ResourceJSON: cs,
	})
	installs := &memTerminologyInstallStore{}
	result, err := EnableCatalogEntry(ctx, CatalogEnableOptions{Global: global, Installs: installs}, store.TerminologyInstallRecord{
		CanonicalURL: "urn:cs", Version: "1", ResourceType: "CodeSystem", Enabled: true, InstalledAt: time.Now().UTC(),
	})
	if err != nil || result.Count != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	enabled, _ := installs.ListEnabled(ctx)
	if len(enabled) != 1 || !enabled[0].Enabled {
		t.Fatalf("enabled=%+v", enabled)
	}
}

func TestEnableCatalogPack(t *testing.T) {
	ctx := context.Background()
	global := NewMemoryStore()
	defs := &memDefinitionStore{}
	cs := []byte(`{"resourceType":"CodeSystem","url":"urn:pack-cs","version":"1","concept":[{"code":"a"}]}`)
	_ = global.PutResource(ctx, store.TerminologyResourceRecord{
		ScopeID: GlobalScopeID, ResourceType: "CodeSystem", CanonicalURL: "urn:pack-cs", Version: "1", ResourceJSON: cs,
	})
	_ = defs.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL: "urn:pack-cs", Version: "1", FHIRResourceType: "CodeSystem",
		PackageName: "test-pack", PackageVersion: "1.0",
	}, nil)
	installs := &memTerminologyInstallStore{}
	result, err := EnableCatalogPack(ctx, CatalogEnableOptions{
		Global: global, Installs: installs, Definitions: defs,
	}, "test-pack", "1.0", true)
	if err != nil || result.Count != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

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
