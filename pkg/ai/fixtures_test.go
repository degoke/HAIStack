package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/degoke/health-ai-stack/pkg/view"
)

var (
	_ store.ResourceStore = (*memResourceStore)(nil)
	_ ai.AuditLogger      = (*fakeAuditLogger)(nil)
	_ ai.ApprovalHook     = (*fakeApprovalHook)(nil)
	_ ai.Deidentifier     = (*fakeDeidentifier)(nil)
	_ ai.ModelAdapter     = (*fakeModelAdapter)(nil)
)

type testHarness struct {
	resources *memResourceStore
	search    *search.Service
	views     *view.Executor
	core      *core.ResourceService
	policy    *ai.AllowListPolicy
	audit     *fakeAuditLogger
	approval  *fakeApprovalHook
	deid      *fakeDeidentifier
	exec      *ai.Executor
	clock     *fixedClock
}

func newTestHarness(t *testing.T, opts harnessOptions) *testHarness {
	t.Helper()
	ctx := context.Background()
	clock := newFixedClock(time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC))
	engine := defaultEngine(t)
	resources := newMemResourceStore()

	if opts.seedPatients {
		resources.Seed(t, patientJane(t), patientJohn(t))
	}
	if opts.seedAppointments {
		resources.Seed(t, appointmentBooked(t))
	}

	var searchSvc *search.Service
	if opts.withSearch {
		snapshot := testSnapshot(t, "Patient")
		reg := search.NewSnapshotRegistry(snapshot)
		indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
			Registry: reg,
			Engine:   engine,
		})
		if err != nil {
			t.Fatalf("NewRegistryIndexer: %v", err)
		}
		searchStore := &memSearchBackend{}
		for _, res := range resources.all() {
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
	if opts.withViews {
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
	if opts.withCore {
		mem := newMemBackend()
		for _, res := range resources.all() {
			if err := mem.Create(ctx, res); err != nil {
				t.Fatalf("seed core: %v", err)
			}
		}
		var coreValidator validate.Validator
		if opts.withValidator {
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
	if opts.allowPatientRead {
		policy.Read["Patient"] = ai.ReadTypePolicy{}
	}
	if opts.allowPatientSearch {
		policy.Search["Patient"] = ai.SearchTypePolicy{AllowedParams: []string{"name", "telecom"}}
	}
	if opts.allowPatientSummaryView {
		policy.Views["patient_summary_view"] = ai.ViewTypePolicy{}
	}
	if opts.allowAppointmentsView {
		policy.Views["appointment_view"] = ai.ViewTypePolicy{}
	}
	if opts.allowPatientWrite {
		policy.Write["Patient"] = ai.WriteTypePolicy{
			CreateFields:   []string{"name", "gender"},
			UpdateFields:   []string{"name"},
			CreateApproval: opts.writeRequiresApproval,
		}
	}

	audit := &fakeAuditLogger{}
	approval := &fakeApprovalHook{approved: opts.approvalGranted}
	deid := &fakeDeidentifier{}

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

	return &testHarness{
		resources: resources,
		search:    searchSvc,
		views:     viewExec,
		core:      coreSvc,
		policy:    policy,
		audit:     audit,
		approval:  approval,
		deid:      deid,
		exec:      exec,
		clock:     clock,
	}
}

type harnessOptions struct {
	seedPatients            bool
	seedAppointments        bool
	withSearch              bool
	withViews               bool
	withCore                bool
	withValidator           bool
	allowPatientRead        bool
	allowPatientSearch      bool
	allowPatientSummaryView bool
	allowAppointmentsView   bool
	allowPatientWrite       bool
	writeRequiresApproval   bool
	approvalGranted         bool
}

type memResourceStore struct {
	mu   sync.Mutex
	data map[string]*types.ResourceEnvelope
}

func newMemResourceStore() *memResourceStore {
	return &memResourceStore{data: make(map[string]*types.ResourceEnvelope)}
}

func resourceKey(resourceType, id string) string { return resourceType + "/" + id }

func (s *memResourceStore) Create(_ context.Context, res *types.ResourceEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; ok {
		return fmt.Errorf("resource already exists: %s", key)
	}
	s.data[key] = res
	return nil
}

func (s *memResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, ok := s.data[resourceKey(resourceType, id)]
	if !ok {
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	return res, nil
}

func (s *memResourceStore) Update(_ context.Context, res *types.ResourceEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resourceKey(res.ResourceType, res.ID)
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("resource not found: %s", key)
	}
	s.data[key] = res
	return nil
}

func (s *memResourceStore) Delete(_ context.Context, resourceType, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, resourceKey(resourceType, id))
	return nil
}

func (s *memResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[resourceKey(resourceType, id)]
	return ok, nil
}

func (s *memResourceStore) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, res := range s.data {
		if res.ResourceType == resourceType {
			ids = append(ids, res.ID)
		}
	}
	if offset >= len(ids) {
		return nil, nil
	}
	end := len(ids)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return ids[offset:end], nil
}

func (s *memResourceStore) Seed(t *testing.T, resources ...*types.ResourceEnvelope) {
	t.Helper()
	ctx := context.Background()
	for _, res := range resources {
		if err := s.Create(ctx, res); err != nil {
			t.Fatalf("seed %s/%s: %v", res.ResourceType, res.ID, err)
		}
	}
}

func (s *memResourceStore) all() []*types.ResourceEnvelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*types.ResourceEnvelope, 0, len(s.data))
	for _, res := range s.data {
		out = append(out, res)
	}
	return out
}

type memSearchBackend struct {
	entries []store.SearchIndexEntry
}

func (m *memSearchBackend) Index(_ context.Context, entry store.SearchIndexEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *memSearchBackend) RemoveIndex(_ context.Context, resourceType, id string) error {
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

func (m *memSearchBackend) Lookup(_ context.Context, key, value string) ([]string, error) {
	return m.LookupMatch(context.Background(), store.SearchMatch{FieldKey: key, Value: value})
}

func (m *memSearchBackend) QueryPrepared(context.Context, store.PreparedQuery, map[string]string) ([]string, error) {
	return nil, nil
}

func (m *memSearchBackend) LookupMatch(_ context.Context, match store.SearchMatch) ([]string, error) {
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

func (m *memSearchBackend) FieldValues(_ context.Context, resourceType, fieldKey string, resourceIDs []string) (map[string]string, error) {
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

type memBackend struct {
	mu        sync.Mutex
	resources map[string]*types.ResourceEnvelope
	history   map[string][]store.ResourceVersion
}

func newMemBackend() *memBackend {
	return &memBackend{
		resources: make(map[string]*types.ResourceEnvelope),
		history:   make(map[string][]store.ResourceVersion),
	}
}

func (m *memBackend) key(resourceType, id string) string { return resourceType + "/" + id }

func (m *memBackend) Create(_ context.Context, res *types.ResourceEnvelope) error {
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

func (m *memBackend) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res, ok := m.resources[m.key(resourceType, id)]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *res
	return &cp, nil
}

func (m *memBackend) Update(_ context.Context, res *types.ResourceEnvelope) error {
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

func (m *memBackend) Delete(_ context.Context, resourceType, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.resources, m.key(resourceType, id))
	return nil
}

func (m *memBackend) Exists(_ context.Context, resourceType, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.resources[m.key(resourceType, id)]
	return ok, nil
}

func (m *memBackend) ListIDs(context.Context, string, int, int) ([]string, error) {
	return nil, nil
}

func (m *memBackend) GetHistory(_ context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]store.ResourceVersion(nil), m.history[m.key(resourceType, id)]...), nil
}

func (m *memBackend) AppendVersion(_ context.Context, version store.ResourceVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.key(version.ResourceType, version.ID)
	m.history[key] = append(m.history[key], version)
	return nil
}

func (m *memBackend) BeginWrite(context.Context) (store.WriteSession, error) {
	return &memWriteSession{backend: m}, nil
}

type memWriteSession struct {
	backend *memBackend
}

func (s *memWriteSession) ResourceStore() store.ResourceStore { return s.backend }
func (s *memWriteSession) HistoryStore() store.HistoryStore   { return s.backend }
func (s *memWriteSession) SearchStore() store.SearchStore     { return nil }
func (s *memWriteSession) EventStore() store.EventStore       { return nil }
func (s *memWriteSession) Commit(context.Context) error       { return nil }
func (s *memWriteSession) Rollback(context.Context) error     { return nil }

type fakeAuditLogger struct {
	mu      sync.Mutex
	records []ai.AuditRecord
}

func (a *fakeAuditLogger) LogToolAccess(_ context.Context, rec ai.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, rec)
	return nil
}

func (a *fakeAuditLogger) Records() []ai.AuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ai.AuditRecord(nil), a.records...)
}

type fakeApprovalHook struct {
	mu       sync.Mutex
	approved bool
	calls    []ai.ApprovalRequest
}

func (h *fakeApprovalHook) RequestApproval(_ context.Context, req ai.ApprovalRequest) (*ai.ApprovalResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, req)
	return &ai.ApprovalResult{Approved: h.approved}, nil
}

type fakeDeidentifier struct {
	called bool
}

func (d *fakeDeidentifier) Deidentify(_ context.Context, req ai.DeidentifyRequest) (any, []string, error) {
	d.called = true
	return req.Data, []string{"phone"}, nil
}

type fakeModelAdapter struct {
	name string
}

func (a *fakeModelAdapter) Name() string { return a.name }

func (a *fakeModelAdapter) Invoke(_ context.Context, req ai.ModelRequest) (*ai.ModelResponse, error) {
	return &ai.ModelResponse{Adapter: a.name, Content: "ok-" + req.Hint}, nil
}

type fixedClock struct {
	fixed time.Time
}

func newFixedClock(t time.Time) *fixedClock { return &fixedClock{fixed: t} }

func (c *fixedClock) Now() time.Time { return c.fixed }

func defaultEngine(t *testing.T) fhirpath.Engine {
	t.Helper()
	eng, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

func googleR4Codec(t *testing.T) *proto.GoogleR4Codec {
	t.Helper()
	return proto.NewGoogleR4Codec()
}

func envelopeFromJSON(t *testing.T, resourceType string, data []byte) *types.ResourceEnvelope {
	t.Helper()
	codec := googleR4Codec(t)
	pb, err := codec.ParseJSONToProto(resourceType, data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	env, err := codec.ProtoToEnvelope(resourceType, pb)
	if err != nil {
		t.Fatalf("ProtoToEnvelope: %v", err)
	}
	return env
}

func patientJane(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-jane",
		"gender": "female",
		"name": [{"given": ["Jane"], "family": "Doe"}],
		"telecom": [{"system": "phone", "value": "555-0100"}]
	}`)
	return envelopeFromJSON(t, "Patient", data)
}

func patientJohn(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Patient",
		"id": "pat-john",
		"gender": "male",
		"name": [{"given": ["John"], "family": "Smith"}]
	}`)
	return envelopeFromJSON(t, "Patient", data)
}

func appointmentBooked(t *testing.T) *types.ResourceEnvelope {
	t.Helper()
	data := []byte(`{
		"resourceType": "Appointment",
		"id": "appt-1",
		"status": "booked",
		"description": "Annual checkup",
		"start": "2024-06-15T09:00:00Z",
		"participant": [{"actor": {"reference": "Patient/pat-jane"}, "status": "accepted"}]
	}`)
	return envelopeFromJSON(t, "Appointment", data)
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
	delete(s.records, defKey(canonicalURL, version))
	return nil
}

func (s *memDefinitionStore) List(context.Context, store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	var out []store.DefinitionResourceRecord
	for _, record := range s.records {
		out = append(out, record)
	}
	return out, nil
}

type memInstallStore struct {
	rows []store.RegistryInstallRecord
}

func newMemInstallStore() *memInstallStore { return &memInstallStore{} }

func (s *memInstallStore) SetEnabled(_ context.Context, record store.RegistryInstallRecord) error {
	s.rows = append(s.rows, record)
	return nil
}

func (s *memInstallStore) UpsertInstall(ctx context.Context, record store.RegistryInstallRecord) error {
	return s.SetEnabled(ctx, record)
}

func (s *memInstallStore) ListEnabled(context.Context) ([]store.RegistryInstallRecord, error) {
	return s.rows, nil
}

func (s *memInstallStore) ListInstalled(context.Context, store.RegistryInstallFilter) ([]store.RegistryInstallRecord, error) {
	return s.rows, nil
}

func (s *memInstallStore) Delete(context.Context, store.RegistryInstallFilter) error { return nil }

func mustValues(params map[string]string) url.Values {
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	return values
}

func dataMap(t *testing.T, data any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}
