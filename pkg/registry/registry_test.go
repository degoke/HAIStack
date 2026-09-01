package registry_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

type memDefinitionStore struct {
	mu      sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
	key := defKey(record.CanonicalURL, record.Version)
	s.records[key] = record
	s.targets[key] = append([]store.DefinitionTargetRecord(nil), targets...)
	return nil
}

func (s *memDefinitionStore) Get(_ context.Context, canonicalURL, version string) (*store.DefinitionResourceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[defKey(canonicalURL, version)]
	if !ok {
		return nil, errors.New("not found")
	}
	copyRecord := record
	return &copyRecord, nil
}

func (s *memDefinitionStore) Delete(_ context.Context, canonicalURL, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := defKey(canonicalURL, version)
	if _, ok := s.records[key]; !ok {
		return errors.New("not found")
	}
	delete(s.records, key)
	delete(s.targets, key)
	return nil
}

func (s *memDefinitionStore) List(_ context.Context, filter store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.DefinitionResourceRecord
	for key, record := range s.records {
		if filter.FHIRVersion != "" && record.FHIRVersion != filter.FHIRVersion {
			continue
		}
		if filter.DefinitionKind != "" && record.DefinitionKind != filter.DefinitionKind {
			continue
		}
		if filter.CanonicalURL != "" && record.CanonicalURL != filter.CanonicalURL {
			continue
		}
		if filter.PackageName != "" && record.PackageName != filter.PackageName {
			continue
		}
		if filter.ModuleName != "" && record.ModuleName != filter.ModuleName {
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
	mu   sync.Mutex
	rows []store.RegistryInstallRecord
}

func newMemInstallStore() *memInstallStore {
	return &memInstallStore{}
}

func (s *memInstallStore) SetEnabled(_ context.Context, record store.RegistryInstallRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		if s.rows[i].DefinitionKind == record.DefinitionKind &&
			s.rows[i].CanonicalURL == record.CanonicalURL &&
			s.rows[i].Version == record.Version &&
			s.rows[i].TargetResourceType == record.TargetResourceType {
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

func (s *memInstallStore) ListEnabled(ctx context.Context) ([]store.RegistryInstallRecord, error) {
	rows, err := s.ListInstalled(ctx, store.RegistryInstallFilter{})
	if err != nil {
		return nil, err
	}
	var out []store.RegistryInstallRecord
	for _, row := range rows {
		if row.Enabled {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *memInstallStore) Delete(_ context.Context, filter store.RegistryInstallFilter) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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

func (s *memInstallStore) ListInstalled(_ context.Context, filter store.RegistryInstallFilter) ([]store.RegistryInstallRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.RegistryInstallRecord
	for _, row := range s.rows {
		if filter.TargetResourceType != "" && row.TargetResourceType != filter.TargetResourceType {
			continue
		}
		if filter.DefinitionKind != "" && row.DefinitionKind != filter.DefinitionKind {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func TestParseDefinitionSearchParameterMultiTarget(t *testing.T) {
	raw := []byte(`{
		"resourceType":"SearchParameter",
		"url":"http://hl7.org/fhir/SearchParameter/individual-gender",
		"version":"4.0.1",
		"name":"gender",
		"status":"active",
		"code":"gender",
		"base":["Patient","Person"],
		"type":"token",
		"expression":"Patient.gender | Person.gender"
	}`)
	parsed, targets, err := registry.ParseDefinition(raw)
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if parsed.DefinitionKind != store.DefinitionKindSearchParameter {
		t.Fatalf("kind = %q", parsed.DefinitionKind)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(targets))
	}
}

func TestRegistryProfileCatalogLoadsBasePatient(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	mgr := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})
	if err := mgr.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := mgr.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}
	snapshot, err := mgr.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	catalog := validate.NewRegistryProfileCatalog(snapshot)
	if err := catalog.Warm(); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	sd, ok := catalog.GetStructureDefinition(validate.BaseStructureDefinitionURL("Patient"))
	if !ok {
		t.Fatal("expected base Patient StructureDefinition")
	}
	if !sd.UseSnapshot {
		t.Fatal("base Patient profile should use snapshot elements")
	}
	if len(sd.Elements) < 40 {
		t.Fatalf("elements = %d, want full snapshot", len(sd.Elements))
	}
}

func TestRegistryProfileCatalogWarmPropagatesParseError(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	mgr := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})
	if err := mgr.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := mgr.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}
	bad := []byte(`{"resourceType":"StructureDefinition","url":"http://hl7.org/fhir/StructureDefinition/Patient","type":"Patient","kind":"resource","snapshot":{"element":"not-an-array"}}`)
	if err := definitions.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL:     validate.BaseStructureDefinitionURL("Patient"),
		Version:          "4.0.1",
		FHIRVersion:      "4.0.1",
		FHIRResourceType: "StructureDefinition",
		DefinitionKind:   store.DefinitionKindStructureDefinition,
		Name:             "Patient",
		Status:           "active",
		JSONData:         bad,
	}, []store.DefinitionTargetRecord{{
		CanonicalURL:       validate.BaseStructureDefinitionURL("Patient"),
		Version:            "4.0.1",
		TargetResourceType: "Patient",
		TargetRole:         "defines",
	}}); err != nil {
		t.Fatalf("Upsert bad definition: %v", err)
	}
	snapshot, err := mgr.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	catalog := validate.NewRegistryProfileCatalog(snapshot)
	if err := catalog.Warm(); err == nil {
		t.Fatal("expected Warm to return profile parse error")
	}
}

func TestParseDefinitionOperationDefinitionBooleanType(t *testing.T) {
	raw := []byte(`{
		"resourceType":"OperationDefinition",
		"url":"http://hl7.org/fhir/uv/sdc/OperationDefinition/Questionnaire-populate",
		"version":"3.0.0",
		"name":"QuestionnairePopulate",
		"status":"active",
		"kind":"operation",
		"code":"populate",
		"system":false,
		"type":true,
		"instance":true,
		"resource":["Questionnaire"]
	}`)
	parsed, targets, err := registry.ParseDefinition(raw)
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if parsed.CanonicalURL != "http://hl7.org/fhir/uv/sdc/OperationDefinition/Questionnaire-populate" {
		t.Fatalf("url = %q", parsed.CanonicalURL)
	}
	if len(targets) != 0 {
		t.Fatalf("targets = %d, want 0", len(targets))
	}
}

func TestParseDefinitionRequiresResourceTypeAndSearchParameterFields(t *testing.T) {
	for name, raw := range map[string][]byte{
		"resourceType": []byte(`{"url":"http://example.org/definition"}`),
		"search code":  []byte(`{"resourceType":"SearchParameter","url":"http://example.org/sp","base":["Patient"],"type":"token"}`),
		"search type":  []byte(`{"resourceType":"SearchParameter","url":"http://example.org/sp","base":["Patient"],"code":"x"}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := registry.ParseDefinition(raw); err == nil {
				t.Fatal("ParseDefinition succeeded; want validation error")
			} else if !errors.Is(err, registry.ErrInvalidDefinition) {
				t.Fatalf("error = %v, want ErrInvalidDefinition", err)
			}
		})
	}
}

func TestSeedBundledDoesNotOverwriteExistingDefinition(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	manager := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})
	custom := []byte(`{"resourceType":"StructureDefinition","id":"Patient","url":"http://hl7.org/fhir/StructureDefinition/Patient","version":"4.0.1","name":"Patient","status":"active","kind":"resource","abstract":false,"type":"Patient","baseDefinition":"http://hl7.org/fhir/StructureDefinition/DomainResource","derivation":"specialization"}`)
	if err := definitions.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL:     "http://hl7.org/fhir/StructureDefinition/Patient",
		Version:          "4.0.1",
		FHIRVersion:      "4.0.1",
		FHIRResourceType: "StructureDefinition",
		DefinitionKind:   store.DefinitionKindStructureDefinition,
		Name:             "Patient",
		Status:           "active",
		ModuleName:       "custom-owner",
		JSONData:         custom,
	}, []store.DefinitionTargetRecord{{CanonicalURL: "http://hl7.org/fhir/StructureDefinition/Patient", Version: "4.0.1", TargetResourceType: "Patient", TargetRole: "defines"}}); err != nil {
		t.Fatalf("seed custom definition: %v", err)
	}
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	record, err := definitions.Get(ctx, "http://hl7.org/fhir/StructureDefinition/Patient", "4.0.1")
	if err != nil {
		t.Fatalf("Get definition: %v", err)
	}
	if string(record.JSONData) != string(custom) || record.ModuleName != "custom-owner" {
		t.Fatal("SeedBundled overwrote an existing definition")
	}
	if err := manager.InstallDefinition(ctx, custom, registry.InstallProvenance{ModuleName: "other-owner", SourceModule: "other-owner"}); err == nil || !errors.Is(err, registry.ErrDefinitionConflict) {
		t.Fatalf("cross-module definition install error = %v, want ErrDefinitionConflict", err)
	}
}

func TestSeedBundledAndCompile(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	manager := registry.NewManager(registry.Config{
		Definitions: definitions,
		Installs:    installs,
		Now:         func() time.Time { return time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC) },
	})

	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	records, err := definitions.List(ctx, store.DefinitionFilter{FHIRVersion: "4.0.1"})
	if err != nil {
		t.Fatalf("List definitions: %v", err)
	}
	if len(records) < 1400 {
		t.Fatalf("seeded definitions = %d, want at least 1400", len(records))
	}
	sds, err := definitions.List(ctx, store.DefinitionFilter{
		FHIRVersion:    "4.0.1",
		DefinitionKind: store.DefinitionKindStructureDefinition,
	})
	if err != nil {
		t.Fatalf("List structure definitions: %v", err)
	}
	if len(sds) < 140 {
		t.Fatalf("structure definitions = %d, want at least 140", len(sds))
	}

	if err := manager.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource Patient: %v", err)
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	if !snapshot.IsResourceEnabled("Patient") {
		t.Fatal("Patient should be enabled")
	}
	if snapshot.IsResourceEnabled("Observation") {
		t.Fatal("Observation should not be enabled")
	}
	params := snapshot.SearchParametersFor("Patient")
	if len(params) < 3 {
		t.Fatalf("Patient search params = %d, want at least 3", len(params))
	}
	expr, ok := snapshot.SearchExpression("Patient", "name")
	if !ok || expr != "Patient.name" {
		t.Fatalf("SearchExpression(name) = (%q, %v)", expr, ok)
	}
	capability := snapshot.CapabilitySnapshot()
	if len(capability.Resources) != 1 || capability.Resources[0].ResourceType != "Patient" {
		t.Fatalf("capability resources = %+v", capability.Resources)
	}
}

func TestCompileRejectsMissingStructureDefinition(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	if err := installs.SetEnabled(ctx, store.RegistryInstallRecord{
		DefinitionKind:     store.DefinitionKindStructureDefinition,
		CanonicalURL:       "http://hl7.org/fhir/StructureDefinition/Missing",
		Version:            "4.0.1",
		TargetResourceType: "Missing",
		Enabled:            true,
		InstalledAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	_, err := registry.CompileSnapshot(ctx, definitions, installs, "4.0.1", time.Now)
	if !errors.Is(err, registry.ErrSnapshotCompile) {
		t.Fatalf("expected ErrSnapshotCompile, got %v", err)
	}
}

func TestSnapshotImplementsValidateRegistry(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	manager := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := manager.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}

	engine, err := validate.NewEngine(validate.Config{
		KnownResourceTypes: map[string]struct{}{"Patient": {}, "Observation": {}},
		InstalledTypes:     snapshot,
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	result, err := engine.Validate(ctx, typesEnvelope("Observation"), validate.ValidateOptions{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("expected Observation validation to fail when not installed")
	}
}

func typesEnvelope(resourceType string) *types.ResourceEnvelope {
	return &types.ResourceEnvelope{
		ResourceType: resourceType,
		ID:           "example-1",
		JSON:         []byte(`{"resourceType":"` + resourceType + `","id":"example-1"}`),
	}
}

func TestDisableResource(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	manager := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := manager.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}
	if err := manager.DisableResource(ctx, "Patient"); err != nil {
		t.Fatalf("DisableResource: %v", err)
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	if snapshot.IsResourceEnabled("Patient") {
		t.Fatal("Patient should be disabled")
	}
}

func TestInstallDefinitionModuleProvenance(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	manager := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})

	raw := []byte(`{
		"resourceType":"SearchParameter",
		"url":"http://example.org/SearchParameter/custom-patient-tag",
		"version":"1.0.0",
		"name":"tag",
		"status":"active",
		"code":"tag",
		"base":["Patient"],
		"type":"token",
		"expression":"Patient.meta.tag"
	}`)
	if err := manager.InstallDefinition(ctx, raw, registry.InstallProvenance{
		PackageName:  "example.module",
		ModuleName:   "patient-tags",
		SourceModule: "patient-tags",
	}); err != nil {
		t.Fatalf("InstallDefinition: %v", err)
	}
	record, err := definitions.Get(ctx, "http://example.org/SearchParameter/custom-patient-tag", "1.0.0")
	if err != nil {
		t.Fatalf("Get installed definition: %v", err)
	}
	if record.ModuleName != "patient-tags" {
		t.Fatalf("ModuleName = %q", record.ModuleName)
	}
	rows, err := installs.ListInstalled(ctx, store.RegistryInstallFilter{
		DefinitionKind: store.DefinitionKindSearchParameter,
	})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(rows) != 1 || rows[0].SourceModule != "patient-tags" {
		t.Fatalf("install rows = %+v", rows)
	}
}

func TestUnknownDefinitionKindStoredAndIgnoredByCompiler(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	manager := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})

	raw := []byte(`{
		"resourceType":"ValueSet",
		"url":"http://example.org/ValueSet/example-status",
		"version":"1.0.0",
		"name":"ExampleStatus",
		"status":"active"
	}`)
	if err := manager.InstallDefinition(ctx, raw, registry.InstallProvenance{}); err != nil {
		t.Fatalf("InstallDefinition ValueSet: %v", err)
	}
	parsed, targets, err := registry.ParseDefinition(raw)
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if parsed.DefinitionKind != store.DefinitionKind("value-set") {
		t.Fatalf("kind = %q", parsed.DefinitionKind)
	}
	if len(targets) != 0 {
		t.Fatalf("targets = %v", targets)
	}

	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	if _, ok := snapshot.DefinitionsByCanonical("http://example.org/ValueSet/example-status", "1.0.0"); !ok {
		t.Fatal("ValueSet should be present in canonical lookup")
	}
	if len(snapshot.ProfilesFor("Patient")) != 0 {
		t.Fatal("profiles bucket should remain empty in MVP compiler")
	}
	if len(snapshot.Operations()) != 0 {
		t.Fatal("operations bucket should remain empty in MVP compiler")
	}
}

func TestCompileRejectsMalformedEnabledStructureDefinition(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	manager := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := manager.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}

	badJSON := []byte(`{"resourceType":"StructureDefinition","url":"http://hl7.org/fhir/StructureDefinition/Patient","version":"4.0.1","name":"Patient","status":"active","kind":"resource","type":"Patient"`)
	parsed, targets, err := registry.ParseDefinition(badJSON)
	if err == nil {
		t.Fatal("expected parse error for malformed JSON")
	}
	_ = parsed
	_ = targets

	if err := definitions.Upsert(ctx, store.DefinitionResourceRecord{
		CanonicalURL:     "http://hl7.org/fhir/StructureDefinition/Patient",
		Version:          "4.0.1",
		FHIRVersion:      "4.0.1",
		FHIRResourceType: "StructureDefinition",
		DefinitionKind:   store.DefinitionKindStructureDefinition,
		Name:             "Patient",
		Status:           "active",
		JSONData:         badJSON,
		InstalledAt:      time.Now().UTC(),
	}, []store.DefinitionTargetRecord{{
		CanonicalURL:       "http://hl7.org/fhir/StructureDefinition/Patient",
		Version:            "4.0.1",
		TargetResourceType: "Patient",
		TargetRole:         "defines",
	}}); err != nil {
		t.Fatalf("Upsert malformed SD: %v", err)
	}

	_, err = manager.RebuildSnapshot(ctx)
	if !errors.Is(err, registry.ErrSnapshotCompile) {
		t.Fatalf("expected ErrSnapshotCompile, got %v", err)
	}
}

func TestDefinitionKindFromResourceType(t *testing.T) {
	if got := store.DefinitionKindFromResourceType("ValueSet"); got != "value-set" {
		t.Fatalf("ValueSet kind = %q", got)
	}
	if got := store.DefinitionKindFromResourceType("StructureDefinition"); got != store.DefinitionKindStructureDefinition {
		t.Fatalf("StructureDefinition kind = %q", got)
	}
}

type recordingReindexNotifier struct {
	calls [][]string
}

func (r *recordingReindexNotifier) ScheduleReindex(_ context.Context, resourceTypes ...string) error {
	r.calls = append(r.calls, append([]string(nil), resourceTypes...))
	return nil
}

func (r *recordingReindexNotifier) ScheduleSearchParameterReindex(_ context.Context, _, _ string, resourceTypes ...string) error {
	r.calls = append(r.calls, append([]string(nil), resourceTypes...))
	return nil
}

func TestEnableResourceSchedulesReindex(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	notifier := &recordingReindexNotifier{}
	manager := registry.NewManager(registry.Config{
		Definitions:   definitions,
		Installs:      installs,
		SearchReindex: notifier,
	})
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := manager.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}
	if len(notifier.calls) != 1 || len(notifier.calls[0]) != 1 || notifier.calls[0][0] != "Patient" {
		t.Fatalf("reindex calls = %#v", notifier.calls)
	}
}

func TestInstallSearchParameterSchedulesReindex(t *testing.T) {
	ctx := context.Background()
	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	notifier := &recordingReindexNotifier{}
	manager := registry.NewManager(registry.Config{
		Definitions:   definitions,
		Installs:      installs,
		SearchReindex: notifier,
	})
	raw := []byte(`{
		"resourceType":"SearchParameter",
		"url":"http://example.org/SearchParameter/custom-tag",
		"version":"1.0.0",
		"name":"CustomTag",
		"status":"active",
		"code":"custom-tag",
		"base":["Patient"],
		"type":"token",
		"expression":"Patient.meta.tag"
	}`)
	if err := manager.InstallDefinition(ctx, raw, registry.InstallProvenance{}); err != nil {
		t.Fatalf("InstallDefinition: %v", err)
	}
	if len(notifier.calls) != 1 || len(notifier.calls[0]) != 1 || notifier.calls[0][0] != "Patient" {
		t.Fatalf("reindex calls = %#v", notifier.calls)
	}
}

func TestInstallDefinitionsFromDir(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	raw := []byte(`{
		"resourceType":"SearchParameter",
		"url":"http://example.org/SearchParameter/dir-import",
		"version":"1.0.0",
		"name":"dir-import",
		"status":"active",
		"code":"dir-import",
		"base":["Patient"],
		"type":"token",
		"expression":"Patient.id"
	}`)
	if err := os.WriteFile(filepath.Join(dir, "custom.json"), raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	definitions := newMemDefinitionStore()
	installs := newMemInstallStore()
	manager := registry.NewManager(registry.Config{Definitions: definitions, Installs: installs})
	if err := manager.InstallDefinitionsFromDir(ctx, dir, registry.InstallProvenance{
		PackageName: "example.custom",
	}); err != nil {
		t.Fatalf("InstallDefinitionsFromDir: %v", err)
	}
	if _, err := definitions.Get(ctx, "http://example.org/SearchParameter/dir-import", "1.0.0"); err != nil {
		t.Fatalf("expected imported definition: %v", err)
	}
}
