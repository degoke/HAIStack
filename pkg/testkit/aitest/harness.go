package aitest

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/testkit/fixtures"
	"github.com/degoke/health-ai-stack/pkg/testkit/storetest"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/degoke/health-ai-stack/pkg/view"
)

// Harness wires an ai.Executor with optional search, views, core, and policy fakes.
type Harness struct {
	Resources *storetest.ResourceStore
	Search    *search.Service
	Views     *view.Executor
	Core      *core.ResourceService
	Policy    *ai.AllowListPolicy
	Audit     *FakeAuditLogger
	Approval  *FakeApprovalHook
	Deid      *FakeDeidentifier
	Model     *FakeModelAdapter
	Executor  *ai.Executor
	Clock     *FixedClock
}

// Options configures which subsystems the harness enables.
type Options struct {
	SeedPatients            bool
	SeedAppointments        bool
	WithSearch              bool
	WithViews               bool
	WithCore                bool
	WithValidator           bool
	AllowPatientRead        bool
	AllowPatientSearch      bool
	AllowPatientSummaryView bool
	AllowAppointmentsView   bool
	AllowPatientWrite       bool
	WriteRequiresApproval   bool
	ApprovalGranted         bool
	FixedTime               time.Time
}

// NewHarness builds an AI executor harness for tests.
func NewHarness(t *testing.T, opts Options) *Harness {
	t.Helper()
	ctx := context.Background()
	clockTime := opts.FixedTime
	if clockTime.IsZero() {
		clockTime = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	}
	clock := NewFixedClock(clockTime)
	engine := defaultEngine(t)
	resources := storetest.NewResourceStore()

	if opts.SeedPatients {
		if err := resources.Seed(ctx, fixtures.PatientJane(t), fixtures.PatientJohn(t)); err != nil {
			t.Fatalf("seed patients: %v", err)
		}
	}
	if opts.SeedAppointments {
		if err := resources.Seed(ctx, fixtures.AppointmentBooked(t)); err != nil {
			t.Fatalf("seed appointments: %v", err)
		}
	}

	var searchSvc *search.Service
	if opts.WithSearch {
		snapshot := testSnapshot(t, "Patient")
		reg := search.NewSnapshotRegistry(snapshot)
		indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
			Registry: reg,
			Engine:   engine,
		})
		if err != nil {
			t.Fatalf("NewRegistryIndexer: %v", err)
		}
		searchStore := &searchBackend{}
		for _, res := range resources.All() {
			entries, err := indexer.Build(ctx, res)
			if err != nil {
				t.Fatalf("index build: %v", err)
			}
			for _, entry := range entries {
				if err := searchStore.Index(ctx, entry); err != nil {
					t.Fatalf("index: %v", err)
				}
			}
		}
		executor := search.NewStoreExecutor(searchStore, resources)
		searchSvc, err = search.NewService(search.ServiceConfig{
			Registry:  reg,
			Executor:  executor,
			Resources: resources,
		})
		if err != nil {
			t.Fatalf("NewService: %v", err)
		}
	}

	var viewExec *view.Executor
	if opts.WithViews {
		viewReg := view.NewRegistry()
		if _, err := viewReg.Register(view.PatientSummaryView(), engine); err != nil {
			t.Fatalf("register patient view: %v", err)
		}
		if _, err := viewReg.Register(view.AppointmentView(), engine); err != nil {
			t.Fatalf("register appointments view: %v", err)
		}
		var err error
		viewExec, err = view.NewExecutor(view.Config{
			Resources: resources,
			Engine:    engine,
			Registry:  viewReg,
			Now:       clock.Now,
		})
		if err != nil {
			t.Fatalf("NewExecutor view: %v", err)
		}
	}

	var coreSvc *core.ResourceService
	var validator validate.Engine
	if opts.WithCore {
		mem := newCoreBackend()
		for _, res := range resources.All() {
			if err := mem.Create(ctx, res); err != nil {
				t.Fatalf("seed core: %v", err)
			}
		}
		var coreValidator validate.Validator
		if opts.WithValidator {
			eng, err := validate.NewEngine(validate.Config{})
			if err != nil {
				t.Fatalf("NewEngine validate: %v", err)
			}
			validator = eng
			coreValidator = validate.NewCoreValidator(eng, validate.ValidateOptions{})
		}
		var err error
		coreSvc, err = core.NewResourceService(core.ResourceServiceConfig{
			Resources: mem,
			History:   mem,
			Sessions:  mem,
			Validator: coreValidator,
		})
		if err != nil {
			t.Fatalf("NewResourceService: %v", err)
		}
	}

	policy := ai.NewAllowListPolicy()
	if opts.AllowPatientRead {
		policy.Read["Patient"] = ai.ReadTypePolicy{}
	}
	if opts.AllowPatientSearch {
		policy.Search["Patient"] = ai.SearchTypePolicy{AllowedParams: []string{"name", "telecom"}}
	}
	if opts.AllowPatientSummaryView {
		policy.Views["patient_summary_view"] = ai.ViewTypePolicy{}
	}
	if opts.AllowAppointmentsView {
		policy.Views["appointment_view"] = ai.ViewTypePolicy{}
	}
	if opts.AllowPatientWrite {
		policy.Write["Patient"] = ai.WriteTypePolicy{
			CreateFields:   []string{"name", "gender"},
			UpdateFields:   []string{"name"},
			CreateApproval: opts.WriteRequiresApproval,
		}
	}

	audit := &FakeAuditLogger{}
	approval := &FakeApprovalHook{Approved: opts.ApprovalGranted}
	deid := &FakeDeidentifier{}
	model := &FakeModelAdapter{AdapterName: "test-model"}

	exec, err := ai.NewExecutor(ai.Config{
		Resources:  resources,
		Search:     searchSvc,
		Views:      viewExec,
		Core:       coreSvc,
		Validator:  validator,
		Policy:     policy,
		Audit:      audit,
		Approval:   approval,
		Deidentify: deid,
		Now:        clock.Now,
	})
	if err != nil {
		t.Fatalf("NewExecutor ai: %v", err)
	}

	return &Harness{
		Resources: resources,
		Search:    searchSvc,
		Views:     viewExec,
		Core:      coreSvc,
		Policy:    policy,
		Audit:     audit,
		Approval:  approval,
		Deid:      deid,
		Model:     model,
		Executor:  exec,
		Clock:     clock,
	}
}

func defaultEngine(t *testing.T) fhirpath.Engine {
	t.Helper()
	eng, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

func testSnapshot(t *testing.T, enabled ...string) *registry.Snapshot {
	t.Helper()
	ctx := context.Background()
	manager := registry.NewManager(registry.Config{
		Definitions: newDefinitionStore(),
		Installs:    newInstallStore(),
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

// URLValues converts a param map to url.Values for search tests.
func URLValues(params map[string]string) url.Values {
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	return values
}

type searchBackend struct {
	entries []store.SearchIndexEntry
}

func (m *searchBackend) Index(_ context.Context, entry store.SearchIndexEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *searchBackend) RemoveIndex(_ context.Context, resourceType, id string) error {
	var kept []store.SearchIndexEntry
	for _, e := range m.entries {
		if e.ResourceType == resourceType && e.ID == id {
			continue
		}
		kept = append(kept, e)
	}
	m.entries = kept
	return nil
}

func (m *searchBackend) Lookup(_ context.Context, key, value string) ([]string, error) {
	return m.LookupMatch(context.Background(), store.SearchMatch{FieldKey: key, Value: value})
}

func (m *searchBackend) QueryPrepared(context.Context, store.PreparedQuery, map[string]string) ([]string, error) {
	return nil, nil
}

func (m *searchBackend) LookupMatch(_ context.Context, match store.SearchMatch) ([]string, error) {
	seen := make(map[string]struct{})
	var ids []string
	for _, entry := range m.entries {
		if match.ResourceType != "" && entry.ResourceType != match.ResourceType {
			continue
		}
		for key, value := range entry.Fields {
			if key == match.FieldKey && value == match.Value {
				if _, ok := seen[entry.ID]; ok {
					continue
				}
				seen[entry.ID] = struct{}{}
				ids = append(ids, entry.ID)
			}
		}
	}
	return ids, nil
}

func (m *searchBackend) FieldValues(_ context.Context, resourceType, fieldKey string, resourceIDs []string) (map[string]string, error) {
	out := make(map[string]string)
	for _, entry := range m.entries {
		if resourceType != "" && entry.ResourceType != resourceType {
			continue
		}
		for key, value := range entry.Fields {
			if key != fieldKey {
				continue
			}
			for _, id := range resourceIDs {
				if entry.ID == id {
					out[id] = value
				}
			}
		}
	}
	return out, nil
}

type coreBackend struct {
	mu        sync.Mutex
	resources map[string]*types.ResourceEnvelope
	history   map[string][]store.ResourceVersion
}

func newCoreBackend() *coreBackend {
	return &coreBackend{
		resources: make(map[string]*types.ResourceEnvelope),
		history:   make(map[string][]store.ResourceVersion),
	}
}

func (m *coreBackend) key(resourceType, id string) string { return resourceType + "/" + id }

func (m *coreBackend) Create(_ context.Context, res *types.ResourceEnvelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(res.ResourceType, res.ID)
	if _, ok := m.resources[k]; ok {
		return fmt.Errorf("resource already exists")
	}
	cp := *res
	m.resources[k] = &cp
	return nil
}

func (m *coreBackend) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res, ok := m.resources[m.key(resourceType, id)]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *res
	return &cp, nil
}

func (m *coreBackend) Update(_ context.Context, res *types.ResourceEnvelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(res.ResourceType, res.ID)
	if _, ok := m.resources[k]; !ok {
		return fmt.Errorf("not found")
	}
	cp := *res
	m.resources[k] = &cp
	return nil
}

func (m *coreBackend) Delete(_ context.Context, resourceType, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.resources, m.key(resourceType, id))
	return nil
}

func (m *coreBackend) Exists(_ context.Context, resourceType, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.resources[m.key(resourceType, id)]
	return ok, nil
}

func (m *coreBackend) ListIDs(context.Context, string, int, int) ([]string, error) {
	return nil, nil
}

func (m *coreBackend) GetHistory(_ context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]store.ResourceVersion(nil), m.history[m.key(resourceType, id)]...), nil
}

func (m *coreBackend) AppendVersion(_ context.Context, version store.ResourceVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.key(version.ResourceType, version.ID)
	m.history[key] = append(m.history[key], version)
	return nil
}

func (m *coreBackend) BeginWrite(context.Context) (store.WriteSession, error) {
	return &coreWriteSession{backend: m}, nil
}

type coreWriteSession struct {
	backend *coreBackend
}

func (s *coreWriteSession) ResourceStore() store.ResourceStore { return s.backend }
func (s *coreWriteSession) HistoryStore() store.HistoryStore   { return s.backend }
func (s *coreWriteSession) SearchStore() store.SearchStore     { return nil }
func (s *coreWriteSession) EventStore() store.EventStore       { return nil }
func (s *coreWriteSession) Commit(context.Context) error       { return nil }
func (s *coreWriteSession) Rollback(context.Context) error     { return nil }
