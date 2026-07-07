package search_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type memDefinitionStore struct {
	records map[string]store.DefinitionResourceRecord
	targets map[string][]store.DefinitionTargetRecord
}

func defKey(url, version string) string { return url + "|" + version }

func newMemDefinitionStore() *memDefinitionStore {
	return &memDefinitionStore{
		records: make(map[string]store.DefinitionResourceRecord),
		targets: make(map[string][]store.DefinitionTargetRecord),
	}
}

func (s *memDefinitionStore) Upsert(_ context.Context, record store.DefinitionResourceRecord, targets []store.DefinitionTargetRecord) error {
	key := defKey(record.CanonicalURL, record.Version)
	s.records[key] = record
	s.targets[key] = append([]store.DefinitionTargetRecord(nil), targets...)
	return nil
}

func (s *memDefinitionStore) Get(_ context.Context, canonicalURL, version string) (*store.DefinitionResourceRecord, error) {
	record, ok := s.records[defKey(canonicalURL, version)]
	if !ok {
		return nil, errors.New("not found")
	}
	copyRecord := record
	return &copyRecord, nil
}

func (s *memDefinitionStore) Delete(_ context.Context, canonicalURL, version string) error {
	key := defKey(canonicalURL, version)
	if _, ok := s.records[key]; !ok {
		return errors.New("not found")
	}
	delete(s.records, key)
	delete(s.targets, key)
	return nil
}

func (s *memDefinitionStore) List(_ context.Context, filter store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	var out []store.DefinitionResourceRecord
	for key, record := range s.records {
		if filter.FHIRVersion != "" && record.FHIRVersion != filter.FHIRVersion {
			continue
		}
		if filter.DefinitionKind != "" && record.DefinitionKind != filter.DefinitionKind {
			continue
		}
		if filter.TargetResourceType != "" {
			matched := false
			for _, target := range s.targets[key] {
				if target.TargetResourceType == filter.TargetResourceType {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, record)
	}
	return out, nil
}

type memInstallStore struct {
	rows []store.RegistryInstallRecord
}

func newMemInstallStore() *memInstallStore { return &memInstallStore{} }

func (s *memInstallStore) SetEnabled(_ context.Context, record store.RegistryInstallRecord) error {
	for i := range s.rows {
		if s.rows[i].TargetResourceType == record.TargetResourceType &&
			s.rows[i].DefinitionKind == record.DefinitionKind {
			s.rows[i] = record
			return nil
		}
	}
	s.rows = append(s.rows, record)
	return nil
}

func (s *memInstallStore) UpsertInstall(ctx context.Context, record store.RegistryInstallRecord) error {
	return s.SetEnabled(ctx, record)
}

func (s *memInstallStore) ListEnabled(context.Context) ([]store.RegistryInstallRecord, error) {
	var out []store.RegistryInstallRecord
	for _, row := range s.rows {
		if row.Enabled {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *memInstallStore) ListInstalled(context.Context, store.RegistryInstallFilter) ([]store.RegistryInstallRecord, error) {
	return append([]store.RegistryInstallRecord(nil), s.rows...), nil
}

func (s *memInstallStore) Delete(_ context.Context, filter store.RegistryInstallFilter) error {
	var kept []store.RegistryInstallRecord
	for _, row := range s.rows {
		if filter.TargetResourceType != "" && row.TargetResourceType != filter.TargetResourceType {
			kept = append(kept, row)
			continue
		}
		if filter.DefinitionKind != "" && row.DefinitionKind != filter.DefinitionKind {
			kept = append(kept, row)
			continue
		}
		if filter.CanonicalURL != "" && row.CanonicalURL != filter.CanonicalURL {
			kept = append(kept, row)
			continue
		}
		if filter.Version != "" && row.Version != filter.Version {
			kept = append(kept, row)
			continue
		}
	}
	s.rows = kept
	return nil
}

func testSnapshot(t *testing.T, enabled ...string) *registry.Snapshot {
	t.Helper()
	ctx := context.Background()
	manager := registry.NewManager(registry.Config{
		Definitions: newMemDefinitionStore(),
		Installs:    newMemInstallStore(),
		Now:         func() time.Time { return time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC) },
	})
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	for _, rt := range enabled {
		if err := manager.EnableResource(ctx, rt); err != nil {
			t.Fatalf("EnableResource %s: %v", rt, err)
		}
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	return snapshot
}

func testEngine(t *testing.T) fhirpath.Engine {
	t.Helper()
	eng, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

func patientResource(t *testing.T, id, family, phone string) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType":"Patient",
		"id":"` + id + `",
		"identifier":[{"system":"http://example.org/mrn","value":"MRN-1"}],
		"name":[{"family":"` + family + `","given":["Jane"]}],
		"birthDate":"1990-05-15",
		"telecom":[{"system":"phone","value":"` + phone + `"}]
	}`)
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	env.LastUpdated = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	return env
}

func observationResource(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType":"Observation",
		"id":"obs-1",
		"status":"final",
		"subject":{"reference":"Patient/pat-1"},
		"encounter":{"reference":"Encounter/enc-1"},
		"code":{"coding":[{"system":"http://loinc.org","code":"8867-4"}],"text":"Heart rate"},
		"effectiveDateTime":"2024-06-01T10:00:00Z"
	}`)
	codec := types.NewJSONCodec()
	env, err := codec.ParseJSON("Observation", data)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	env.LastUpdated = time.Date(2024, 6, 2, 8, 0, 0, 0, time.UTC)
	return env
}

func fieldValues(entries []store.SearchIndexEntry) map[string][]string {
	out := make(map[string][]string)
	for _, entry := range entries {
		for k, v := range entry.Fields {
			out[k] = append(out[k], v)
		}
	}
	return out
}

func containsValue(values map[string][]string, key, want string) bool {
	for _, v := range values[key] {
		if v == want {
			return true
		}
	}
	return false
}
