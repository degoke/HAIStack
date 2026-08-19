package aitest

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/testkit/fixtures"
	"github.com/degoke/health-ai-stack/pkg/testkit/storetest"
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
	var indexedStore *storetest.SearchStore
	var searchIndexer search.Indexer
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
		searchIndexer = indexer
		indexedStore = storetest.NewSearchStore()
		for _, res := range resources.All() {
			entries, err := indexer.Build(ctx, res)
			if err != nil {
				t.Fatalf("index build: %v", err)
			}
			for _, entry := range entries {
				if err := indexedStore.Index(ctx, entry); err != nil {
					t.Fatalf("index: %v", err)
				}
			}
		}
		executor := search.NewStoreExecutor(indexedStore, resources)
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
		sessions := storetest.NewWriteSessionProvider()
		// Share the current resource store with AI reads so writes performed by
		// Core are immediately visible through the harness read path.
		sessions.Resources = resources
		if indexedStore != nil {
			sessions.Search = indexedStore
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
			Resources: resources,
			History:   sessions.History,
			Sessions:  sessions,
			Validator: coreValidator,
			Indexer:   searchIndexer,
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
			UpdateApproval: opts.WriteRequiresApproval,
		}
	}

	audit := &FakeAuditLogger{}
	approvalStore := ai.NewMemoryApprovalStore()
	approval := &FakeApprovalHook{Approved: opts.ApprovalGranted, Store: approvalStore}
	deid := &FakeDeidentifier{}
	model := &FakeModelAdapter{AdapterName: "test-model"}

	exec, err := ai.NewExecutor(ai.Config{
		Resources:     resources,
		Search:        searchSvc,
		Views:         viewExec,
		Core:          coreSvc,
		Validator:     validator,
		Policy:        policy,
		Audit:         audit,
		Approval:      approval,
		ApprovalStore: approvalStore,
		Deidentify:    deid,
		ModelRouter:   &ai.ModelRouter{Local: model},
		Now:           clock.Now,
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
