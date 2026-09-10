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

func (s *memTerminologyInstallStore) ListInstalled(_ context.Context, _ store.TerminologyInstallFilter) ([]store.TerminologyInstallRecord, error) {
	return append([]store.TerminologyInstallRecord(nil), s.rows...), nil
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
	if err := mgr.DeleteDefinition(ctx, "urn:test-cs", "1.0"); err != nil {
		t.Fatal(err)
	}
	if len(installs.rows) != 0 {
		t.Fatalf("install rows after delete=%v", installs.rows)
	}
}
