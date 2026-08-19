package modules_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/modules"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/testkit/storetest"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// Memory store implementations for fast manager tests.

type memDefinitionStore struct {
	mu      sync.Mutex
	data    map[string]store.DefinitionResourceRecord
	targets map[string][]store.DefinitionTargetRecord
}

func newMemDefinitionStore() *memDefinitionStore {
	return &memDefinitionStore{
		data:    make(map[string]store.DefinitionResourceRecord),
		targets: make(map[string][]store.DefinitionTargetRecord),
	}
}

func (s *memDefinitionStore) Upsert(_ context.Context, record store.DefinitionResourceRecord, targets []store.DefinitionTargetRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := record.CanonicalURL + "|" + record.Version
	s.data[key] = record
	s.targets[key] = append([]store.DefinitionTargetRecord(nil), targets...)
	return nil
}

func (s *memDefinitionStore) Get(_ context.Context, canonicalURL, version string) (*store.DefinitionResourceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := canonicalURL + "|" + version
	record, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("definition not found: %s", key)
	}
	copy := record
	return &copy, nil
}

func (s *memDefinitionStore) List(_ context.Context, filter store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.DefinitionResourceRecord
	for _, record := range s.data {
		if filter.FHIRVersion != "" && record.FHIRVersion != filter.FHIRVersion {
			continue
		}
		if filter.DefinitionKind != "" && record.DefinitionKind != filter.DefinitionKind {
			continue
		}
		if filter.CanonicalURL != "" && record.CanonicalURL != filter.CanonicalURL {
			continue
		}
		if filter.ModuleName != "" && record.ModuleName != filter.ModuleName {
			continue
		}
		if filter.TargetResourceType != "" {
			key := record.CanonicalURL + "|" + record.Version
			found := false
			for _, target := range s.targets[key] {
				if target.TargetResourceType == filter.TargetResourceType {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		copy := record
		out = append(out, copy)
	}
	return out, nil
}

func (s *memDefinitionStore) Delete(_ context.Context, canonicalURL, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := canonicalURL + "|" + version
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("definition not found: %s", key)
	}
	delete(s.data, key)
	delete(s.targets, key)
	return nil
}

type memRegistryInstallStore struct {
	mu   sync.Mutex
	rows []store.RegistryInstallRecord
}

func newMemRegistryInstallStore() *memRegistryInstallStore {
	return &memRegistryInstallStore{}
}

func (s *memRegistryInstallStore) SetEnabled(_ context.Context, record store.RegistryInstallRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, row := range s.rows {
		if row.DefinitionKind == record.DefinitionKind &&
			row.CanonicalURL == record.CanonicalURL &&
			row.Version == record.Version &&
			row.TargetResourceType == record.TargetResourceType {
			s.rows[i] = record
			return nil
		}
	}
	s.rows = append(s.rows, record)
	return nil
}

func (s *memRegistryInstallStore) UpsertInstall(_ context.Context, record store.RegistryInstallRecord) error {
	return s.SetEnabled(context.Background(), record)
}

func (s *memRegistryInstallStore) ListEnabled(_ context.Context) ([]store.RegistryInstallRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.RegistryInstallRecord
	for _, row := range s.rows {
		if row.Enabled {
			copy := row
			out = append(out, copy)
		}
	}
	return out, nil
}

func (s *memRegistryInstallStore) ListInstalled(_ context.Context, filter store.RegistryInstallFilter) ([]store.RegistryInstallRecord, error) {
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
		copy := row
		out = append(out, copy)
	}
	return out, nil
}

func (s *memRegistryInstallStore) Delete(_ context.Context, filter store.RegistryInstallFilter) error {
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

func newTestManager() *modules.Manager {
	return newTestManagerWithAuthorizer(nil)
}

func newTestManagerWithAuthorizer(authorizer modules.InstallAuthorizer) *modules.Manager {
	modulesStore := newMemModuleStore()
	defsStore := newMemDefinitionStore()
	installsStore := newMemRegistryInstallStore()
	reg := registry.NewManager(registry.Config{
		Definitions: defsStore,
		Installs:    installsStore,
	})
	return modules.NewManager(modules.Config{
		ModuleStore:          modulesStore,
		DefinitionStore:      defsStore,
		RegistryInstallStore: installsStore,
		RegistryManager:      reg,
		Authorizer:           authorizer,
		Now:                  func() time.Time { return time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC) },
	})
}

func TestManagerInstallCoreEnablesResources(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	result, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core"))
	if err != nil {
		t.Fatalf("Install core: %v", err)
	}
	if result.Name != "core" || result.Version != "1.0.0" {
		t.Errorf("result = %+v, want core 1.0.0", result)
	}
	wantResources := []string{"Organization", "Patient", "Practitioner"}
	if got := sortedCopy(result.EnabledResources); !sliceEqual(got, wantResources) {
		t.Errorf("enabled resources = %v, want %v", got, wantResources)
	}
	if result.Snapshot == nil || !result.Snapshot.IsResourceEnabled("Patient") {
		t.Fatal("Patient should be enabled in snapshot")
	}
	params := result.Snapshot.SearchParametersFor("Patient")
	found := false
	for _, p := range params {
		if p.Code == "identifier-custom" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Patient custom identifier search parameter not found in snapshot")
	}
}

func TestManagerInstallSchedulingAfterCore(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core")); err != nil {
		t.Fatalf("Install core: %v", err)
	}
	result, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "scheduling"))
	if err != nil {
		t.Fatalf("Install scheduling: %v", err)
	}
	wantResources := []string{"Appointment", "Schedule", "Slot"}
	if got := sortedCopy(result.EnabledResources); !sliceEqual(got, wantResources) {
		t.Errorf("enabled resources = %v, want %v", got, wantResources)
	}
	if !result.Snapshot.IsResourceEnabled("Appointment") {
		t.Fatal("Appointment should be enabled")
	}
	params := result.Snapshot.SearchParametersFor("Appointment")
	found := false
	for _, p := range params {
		if p.Code == "date-custom" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Appointment custom date search parameter not found")
	}
}

func TestManagerInstallClinicalLiteFailsWithoutCore(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	_, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "clinical-lite"))
	if err == nil {
		t.Fatal("expected dependency error")
	}
	if !isError(err, modules.ErrMissingDependency) {
		t.Errorf("error = %v, want ErrMissingDependency", err)
	}
}

func TestManagerInstallRollsBackRegistryChangesOnApplyFailure(t *testing.T) {
	ctx := context.Background()
	moduleStore := newMemModuleStore()
	defs := newMemDefinitionStore()
	installs := newMemRegistryInstallStore()
	reg := registry.NewManager(registry.Config{Definitions: defs, Installs: installs})
	mgr := modules.NewManager(modules.Config{
		ModuleStore:          moduleStore,
		DefinitionStore:      defs,
		RegistryInstallStore: installs,
		RegistryManager:      reg,
	})

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "module.json"), []byte(`{
		"name":"broken","version":"1.0.0",
		"resources":["Appointment","NotARealFHIRResource"]
	}`))
	if _, err := mgr.Install(ctx, dir); err == nil {
		t.Fatal("expected install failure")
	}
	if got := len(moduleStore.modules); got != 0 {
		t.Fatalf("modules after failed install = %d, want 0", got)
	}
	rows, err := installs.ListInstalled(ctx, store.RegistryInstallFilter{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("registry installs after failed install = %d, want 0", len(rows))
	}
}

func TestManagerUpgradeUsesCandidateManifestForCycleDetection(t *testing.T) {
	ctx := context.Background()
	moduleStore := newMemModuleStore()
	registerModule(t, moduleStore, "a", "1.0.0", nil)
	registerModule(t, moduleStore, "b", "1.0.0", []modules.DependencyRef{{Name: "a", Version: "1.0.0"}})
	defs := newMemDefinitionStore()
	installs := newMemRegistryInstallStore()
	reg := registry.NewManager(registry.Config{Definitions: defs, Installs: installs})
	mgr := modules.NewManager(modules.Config{
		ModuleStore:          moduleStore,
		DefinitionStore:      defs,
		RegistryInstallStore: installs,
		RegistryManager:      reg,
	})

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "module.json"), []byte(`{
		"name":"a","version":"1.1.0",
		"dependencies":[{"name":"b","version":"1.0.0"}]
	}`))
	if _, err := mgr.Upgrade(ctx, dir); err == nil {
		t.Fatal("expected circular dependency error")
	} else if !isError(err, modules.ErrCircularDependency) {
		t.Fatalf("error = %v, want ErrCircularDependency", err)
	}
}

func TestManagerUninstallRejectsPersistedResources(t *testing.T) {
	ctx := context.Background()
	moduleStore := newMemModuleStore()
	defs := newMemDefinitionStore()
	installs := newMemRegistryInstallStore()
	resources := storetest.NewResourceStore()
	reg := registry.NewManager(registry.Config{Definitions: defs, Installs: installs})
	mgr := modules.NewManager(modules.Config{
		ModuleStore:          moduleStore,
		DefinitionStore:      defs,
		RegistryInstallStore: installs,
		RegistryManager:      reg,
		ResourceStore:        resources,
	})

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "module.json"), []byte(`{"name":"patients","version":"1.0.0","resources":["Patient"]}`))
	if _, err := mgr.Install(ctx, dir); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := resources.Create(ctx, &types.ResourceEnvelope{ResourceType: "Patient", ID: "p1"}); err != nil {
		t.Fatalf("create resource: %v", err)
	}
	if err := mgr.Uninstall(ctx, "patients"); err == nil || !isError(err, modules.ErrResourceTypeInUse) {
		t.Fatalf("Uninstall error = %v, want ErrResourceTypeInUse", err)
	}
	if _, err := mgr.Inspect(ctx, "patients"); err != nil {
		t.Fatalf("module should remain installed after blocked uninstall: %v", err)
	}
}

func TestManagerListAndInspect(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core")); err != nil {
		t.Fatalf("Install core: %v", err)
	}

	installed, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(installed) != 1 || installed[0].Name != "core" {
		t.Fatalf("List = %+v, want one core module", installed)
	}
	if len(installed[0].Deferred.Views) != 1 || installed[0].Deferred.Views[0] != "PatientDashboard" {
		t.Errorf("views = %v, want [PatientDashboard]", installed[0].Deferred.Views)
	}
	if len(installed[0].Definitions) != 1 {
		t.Errorf("definitions = %d, want 1", len(installed[0].Definitions))
	}

	inspected, err := mgr.Inspect(ctx, "core")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if inspected.Name != "core" || inspected.Version != "1.0.0" {
		t.Errorf("inspected = %+v, want core 1.0.0", inspected)
	}

	_, err = mgr.Inspect(ctx, "missing")
	if err == nil {
		t.Fatal("expected error for missing module")
	}
	if !isError(err, modules.ErrModuleNotFound) {
		t.Errorf("error = %v, want ErrModuleNotFound", err)
	}
}

func TestManagerPlanInstall(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	plan, err := mgr.PlanInstall(ctx, filepath.Join("..", "..", "modules", "core"))
	if err != nil {
		t.Fatalf("PlanInstall: %v", err)
	}
	if plan.Name != "core" || plan.Version != "1.0.0" || plan.Action != "install" {
		t.Errorf("plan = %+v, want core 1.0.0 install", plan)
	}
	if len(plan.ResourcesToEnable) != 3 {
		t.Errorf("resources to enable = %d, want 3", len(plan.ResourcesToEnable))
	}
	if len(plan.DefinitionsToInstall) != 1 {
		t.Errorf("definitions to install = %d, want 1", len(plan.DefinitionsToInstall))
	}
	if plan.Deferred.Views[0] != "PatientDashboard" {
		t.Errorf("deferred view = %v, want PatientDashboard", plan.Deferred.Views)
	}
}

func TestManagerInstallCallsAuthorizer(t *testing.T) {
	calls := 0
	var got modules.InstallAuthRequest
	mgr := newTestManagerWithAuthorizer(modules.InstallAuthorizerFunc(func(_ context.Context, req modules.InstallAuthRequest) error {
		calls++
		got = req
		return nil
	}))
	ctx := context.Background()
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core")); err != nil {
		t.Fatalf("Install core: %v", err)
	}
	if calls != 1 {
		t.Fatalf("authorizer calls = %d, want 1", calls)
	}
	if got.Action != "install" || got.Module.Manifest.Name != "core" || got.Plan == nil {
		t.Fatalf("request = %+v, want install core with plan", got)
	}
}

func TestManagerInstallDeniedByAuthorizer(t *testing.T) {
	wantErr := fmt.Errorf("denied")
	mgr := newTestManagerWithAuthorizer(modules.InstallAuthorizerFunc(func(_ context.Context, req modules.InstallAuthRequest) error {
		if req.Module.Manifest.Name != "core" {
			t.Fatalf("module name = %q, want core", req.Module.Manifest.Name)
		}
		return wantErr
	}))
	ctx := context.Background()
	_, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core"))
	if err == nil {
		t.Fatal("expected authorization error")
	}
	if err != wantErr {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestManagerUpgradeAdditive(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core")); err != nil {
		t.Fatalf("Install core 1.0.0: %v", err)
	}

	upgradeDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(upgradeDir, "definitions"), 0755); err != nil {
		t.Fatalf("mkdir definitions: %v", err)
	}
	mustWriteFile(t, filepath.Join(upgradeDir, "module.json"), []byte(`{
		"name":"core","version":"1.1.0",
		"resources":["Patient","Practitioner","Organization","RelatedPerson"],
		"definitionFiles":["definitions/Patient-identifier-custom.json","definitions/RelatedPerson-search-name.json"],
		"views":["PatientDashboard"]
	}`))
	mustWriteFile(t, filepath.Join(upgradeDir, "definitions", "Patient-identifier-custom.json"), []byte(`{
		"resourceType":"SearchParameter","id":"Patient-identifier-custom",
		"url":"http://haistack.example.org/SearchParameter/Patient-identifier-custom",
		"version":"1.0.0","name":"identifier-custom","status":"draft","code":"identifier-custom",
		"base":["Patient"],"type":"token","expression":"Patient.identifier"
	}`))
	mustWriteFile(t, filepath.Join(upgradeDir, "definitions", "RelatedPerson-search-name.json"), []byte(`{
		"resourceType":"SearchParameter","id":"RelatedPerson-name-custom",
		"url":"http://haistack.example.org/SearchParameter/RelatedPerson-name-custom",
		"version":"1.0.0","name":"name-custom","status":"draft","code":"name-custom",
		"base":["RelatedPerson"],"type":"string","expression":"RelatedPerson.name"
	}`))

	result, err := mgr.Upgrade(ctx, upgradeDir)
	if err != nil {
		t.Fatalf("Upgrade core: %v", err)
	}
	if result.OldVersion != "1.0.0" || result.NewVersion != "1.1.0" {
		t.Errorf("old=%s new=%s, want 1.0.0 -> 1.1.0", result.OldVersion, result.NewVersion)
	}
	if !sliceEqual(sortedCopy(result.EnabledResources), []string{"RelatedPerson"}) {
		t.Errorf("new enabled resources = %v, want [RelatedPerson]", result.EnabledResources)
	}
	if len(result.InstalledDefinitions) != 1 {
		t.Errorf("new installed definitions = %d, want 1", len(result.InstalledDefinitions))
	}
	if !result.Snapshot.IsResourceEnabled("RelatedPerson") {
		t.Fatal("RelatedPerson should be enabled after upgrade")
	}
}

func TestManagerUpgradeDowngradeBlocked(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core")); err != nil {
		t.Fatalf("Install core: %v", err)
	}
	upgradeDir := t.TempDir()
	mustWriteFile(t, filepath.Join(upgradeDir, "module.json"), []byte(`{"name":"core","version":"0.9.0","resources":["Patient"]}`))
	_, err := mgr.Upgrade(ctx, upgradeDir)
	if err == nil {
		t.Fatal("expected downgrade error")
	}
	if !isError(err, modules.ErrDowngradeNotAllowed) {
		t.Errorf("error = %v, want ErrDowngradeNotAllowed", err)
	}
}

func TestManagerUpgradeRemovalBlocked(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core")); err != nil {
		t.Fatalf("Install core: %v", err)
	}
	upgradeDir := t.TempDir()
	mustWriteFile(t, filepath.Join(upgradeDir, "module.json"), []byte(`{"name":"core","version":"1.1.0","resources":["Patient","Practitioner"]}`))
	_, err := mgr.Upgrade(ctx, upgradeDir)
	if err == nil {
		t.Fatal("expected removal error")
	}
	if !isError(err, modules.ErrUpgradeWouldRemove) {
		t.Errorf("error = %v, want ErrUpgradeWouldRemove", err)
	}
}

func TestManagerUninstallBlockedByDependency(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core")); err != nil {
		t.Fatalf("Install core: %v", err)
	}
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "scheduling")); err != nil {
		t.Fatalf("Install scheduling: %v", err)
	}
	if err := mgr.Uninstall(ctx, "core"); err == nil {
		t.Fatal("expected uninstall blocked")
	} else if !isError(err, modules.ErrModuleInUse) {
		t.Errorf("error = %v, want ErrModuleInUse", err)
	}
}

func TestManagerUninstallIndependentModule(t *testing.T) {
	mgr := newTestManager()
	ctx := context.Background()
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "core")); err != nil {
		t.Fatalf("Install core: %v", err)
	}
	if _, err := mgr.Install(ctx, filepath.Join("..", "..", "modules", "scheduling")); err != nil {
		t.Fatalf("Install scheduling: %v", err)
	}
	if err := mgr.Uninstall(ctx, "scheduling"); err != nil {
		t.Fatalf("Uninstall scheduling: %v", err)
	}
	installed, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(installed) != 1 || installed[0].Name != "core" {
		t.Fatalf("List = %+v, want only core", installed)
	}
	for _, def := range installed[0].Definitions {
		if def.CanonicalURL == "http://haistack.example.org/SearchParameter/Appointment-date-custom" {
			t.Fatal("scheduling definition should not be present on core")
		}
	}
}

func sortedCopy(xs []string) []string {
	out := make([]string, len(xs))
	copy(out, xs)
	sort.Strings(out)
	return out
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
