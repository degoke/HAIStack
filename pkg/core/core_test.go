package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/sdc"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

func TestCreateWithCallerID(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{outbox: true, indexer: true})
	ctx := context.Background()

	created, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != "pat-1" {
		t.Fatalf("ID = %q, want pat-1", created.ID)
	}
	if created.VersionID == "" {
		t.Fatal("expected versionId to be assigned")
	}
	if created.LastUpdated.IsZero() {
		t.Fatal("expected lastUpdated to be assigned")
	}
}

func TestCreateWithGeneratedID(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()

	created, err := harness.svc.Create(ctx, patientEnvelope("", "Doe"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated id")
	}
}

func TestDuplicateCreateConflictNoPartialWrites(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{outbox: true, indexer: true})
	ctx := context.Background()

	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Duplicate"))
	if !core.IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}

	history, err := harness.svc.History(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	if len(harness.mem.events) != 1 {
		t.Fatalf("events length = %d, want 1 from first create only", len(harness.mem.events))
	}
	if len(harness.mem.search) != 1 {
		t.Fatalf("search entries = %d, want 1 from first create only", len(harness.mem.search))
	}
}

func TestReadExistingAndMissing(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()

	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	read, err := harness.svc.Read(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if read.ID != "pat-1" {
		t.Fatalf("Read ID = %q", read.ID)
	}

	_, err = harness.svc.Read(ctx, "Patient", "missing")
	if !core.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestUpdateExistingAndMissing(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{outbox: true})
	ctx := context.Background()

	created, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updatedEnv := patientEnvelope("pat-1", "Smith")
	updated, err := harness.svc.Update(ctx, updatedEnv)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.VersionID == created.VersionID {
		t.Fatal("expected new versionId on update")
	}
	if updated.LastUpdated.Before(created.LastUpdated) || updated.LastUpdated.Equal(created.LastUpdated) {
		t.Fatal("expected lastUpdated to advance")
	}

	_, err = harness.svc.Update(ctx, patientEnvelope("missing", "Smith"))
	if !core.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestDeleteExistingAndMissing(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{outbox: true, indexer: true})
	ctx := context.Background()

	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := harness.svc.Delete(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := harness.svc.Read(ctx, "Patient", "pat-1")
	if !core.IsNotFound(err) {
		t.Fatalf("expected not found after delete, got %v", err)
	}

	history, err := harness.svc.History(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	last := history[len(history)-1]
	if last.Action != store.VersionActionDelete || !last.Deleted {
		t.Fatalf("last history = %+v", last)
	}

	if err := harness.svc.Delete(ctx, "Patient", "pat-1"); err == nil {
		t.Fatal("expected not found on second delete")
	}
}

func TestPatchReplacesField(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()

	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	patch := []byte(`[{"op":"replace","path":"/name/0/family","value":"Smith"}]`)
	patched, err := harness.svc.Patch(ctx, "Patient", "pat-1", patch)
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if patched.ID != "pat-1" {
		t.Fatalf("patched id = %q", patched.ID)
	}

	read, err := harness.svc.Read(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(read.JSON, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	names := obj["name"].([]interface{})
	first := names[0].(map[string]interface{})
	if first["family"] != "Smith" {
		t.Fatalf("family = %v, want Smith", first["family"])
	}
}

func TestPatchMissingResource(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()

	patch := []byte(`[{"op":"replace","path":"/name/0/family","value":"Smith"}]`)
	_, err := harness.svc.Patch(ctx, "Patient", "missing", patch)
	if !core.IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestPatchInvalidOperation(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()

	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	patch := []byte(`[{"op":"replace","path":"/name/0/missing","value":"Smith"}]`)
	_, err := harness.svc.Patch(ctx, "Patient", "pat-1", patch)
	if err == nil || core.KindOf(err) != core.ErrorKindInvalid {
		t.Fatalf("expected invalid error, got %v", err)
	}
}

func TestPatchCannotChangeResourceIdentity(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()
	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, patch := range []string{
		`[{"op":"replace","path":"/id","value":"pat-2"}]`,
		`[{"op":"remove","path":"/id"}]`,
	} {
		_, err := harness.svc.Patch(ctx, "Patient", "pat-1", []byte(patch))
		if err == nil || core.KindOf(err) != core.ErrorKindInvalid {
			t.Fatalf("patch %s: expected invalid identity error, got %v", patch, err)
		}
	}

	if _, err := harness.svc.Read(ctx, "Patient", "pat-2"); !core.IsNotFound(err) {
		t.Fatalf("unexpected resource created at patched id: %v", err)
	}
}

func TestValidatorFailureAbortsWrite(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{
		validator: &failValidator{err: errors.New("invalid patient")},
	})
	ctx := context.Background()

	_, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err == nil || core.KindOf(err) != core.ErrorKindInvalid {
		t.Fatalf("expected invalid error, got %v", err)
	}
	if harness.mem.resourceCount() != 0 {
		t.Fatalf("resource count = %d, want 0", harness.mem.resourceCount())
	}
}

func TestBuiltinValidatorAbortsWrite(t *testing.T) {
	eng, err := validate.NewEngine(validate.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	harness := newTestHarness(t, harnessOptions{
		validator: validate.NewCoreValidator(eng, validate.ValidateOptions{}),
	})
	ctx := context.Background()

	invalid := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"bad id!"}`),
	}
	_, err = harness.svc.Create(ctx, invalid)
	if err == nil || core.KindOf(err) != core.ErrorKindInvalid {
		t.Fatalf("expected invalid error, got %v", err)
	}

	outcome := core.OperationOutcomeFromError(err)
	if outcome == nil || len(outcome.Issue) == 0 {
		t.Fatal("expected operation outcome")
	}
	if outcome.Issue[0].Code != "invalid-id" {
		t.Fatalf("issue code = %q, want invalid-id", outcome.Issue[0].Code)
	}
	if !strings.Contains(outcome.Issue[0].Diagnostics, "FHIR id syntax") {
		t.Fatalf("diagnostics = %q", outcome.Issue[0].Diagnostics)
	}
	if harness.mem.resourceCount() != 0 {
		t.Fatalf("resource count = %d, want 0", harness.mem.resourceCount())
	}
}

func TestIndexerFailureAbortsWrite(t *testing.T) {
	mem := newMemBackend()
	svc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: mem,
		History:   mem,
		Sessions:  mem,
		IDPolicy:  core.DefaultIDPolicy{},
		Indexer:   &failIndexer{err: errors.New("index failed")},
	})
	if err != nil {
		t.Fatalf("NewResourceService: %v", err)
	}
	ctx := context.Background()

	_, err = svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err == nil || core.KindOf(err) != core.ErrorKindException {
		t.Fatalf("expected exception, got %v", err)
	}
	if mem.resourceCount() != 0 {
		t.Fatalf("resource count = %d, want 0", mem.resourceCount())
	}
}

func TestOutboxFailureAbortsWrite(t *testing.T) {
	mem := newMemBackend()
	svc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: mem,
		History:   mem,
		Sessions:  mem,
		IDPolicy:  core.DefaultIDPolicy{},
		Outbox:    outboxFailer{},
	})
	if err != nil {
		t.Fatalf("NewResourceService: %v", err)
	}
	ctx := context.Background()

	_, err = svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err == nil || core.KindOf(err) != core.ErrorKindException {
		t.Fatalf("expected exception, got %v", err)
	}
	if mem.resourceCount() != 0 {
		t.Fatalf("resource count = %d, want 0", mem.resourceCount())
	}
}

type outboxFailer struct{}

func (outboxFailer) Append(context.Context, store.ResourceEvent) (store.ResourceEvent, error) {
	return store.ResourceEvent{}, errors.New("outbox append failed")
}

func TestDeleteEventUsesTombstoneVersionID(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{outbox: true})
	ctx := context.Background()

	created, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := harness.svc.Delete(ctx, "Patient", "pat-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	history, err := harness.svc.History(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	tombstone := history[len(history)-1]
	if tombstone.VersionID == "" || tombstone.VersionID == created.VersionID {
		t.Fatalf("tombstone version = %q, want new id", tombstone.VersionID)
	}

	if len(harness.mem.events) != 2 {
		t.Fatalf("events length = %d, want 2", len(harness.mem.events))
	}
	deleteEvent := harness.mem.events[1]
	if deleteEvent.Action != store.EventActionDelete {
		t.Fatalf("delete event action = %q", deleteEvent.Action)
	}
	if deleteEvent.VersionID != tombstone.VersionID {
		t.Fatalf("delete event versionId = %q, want tombstone %q", deleteEvent.VersionID, tombstone.VersionID)
	}
	if deleteEvent.Hash != created.Hash {
		t.Fatalf("delete event hash = %q, want content hash %q", deleteEvent.Hash, created.Hash)
	}
}

func TestHistoryOrderedVersions(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()

	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "One")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := harness.svc.Update(ctx, patientEnvelope("pat-1", "Two")); err != nil {
		t.Fatalf("Update: %v", err)
	}

	history, err := harness.svc.History(ctx, "Patient", "pat-1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	if history[0].Action != store.VersionActionCreate {
		t.Fatalf("first action = %q", history[0].Action)
	}
	if history[1].Action != store.VersionActionUpdate {
		t.Fatalf("second action = %q", history[1].Action)
	}
}

func TestTransactionBundleMixedMethods(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{outbox: true, indexer: true})
	ctx := context.Background()

	bundleJSON := []byte(`{
		"resourceType":"Bundle",
		"type":"transaction",
		"entry":[
			{"request":{"method":"POST","url":"Patient"},"resource":{"resourceType":"Patient","name":[{"family":"A"}]}},
			{"request":{"method":"PUT","url":"Patient/pat-seed"},"resource":{"resourceType":"Patient","id":"pat-seed","name":[{"family":"B"}]}},
			{"request":{"method":"DELETE","url":"Patient/pat-seed"}}
		]
	}`)

	if _, err := harness.svc.Create(ctx, patientEnvelope("pat-seed", "Seed")); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	resp, err := harness.svc.ProcessTransactionBundle(ctx, &types.ResourceEnvelope{
		ResourceType: "Bundle",
		JSON:         bundleJSON,
	})
	if err != nil {
		t.Fatalf("ProcessTransactionBundle: %v", err)
	}

	var bundleObj map[string]interface{}
	if err := json.Unmarshal(resp.JSON, &bundleObj); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if bundleObj["type"] != "transaction-response" {
		t.Fatalf("response type = %v", bundleObj["type"])
	}
	entries, ok := bundleObj["entry"].([]interface{})
	if !ok || len(entries) != 3 {
		t.Fatalf("response entries = %v", bundleObj["entry"])
	}

	_, err = harness.svc.Read(ctx, "Patient", "pat-seed")
	if !core.IsNotFound(err) {
		t.Fatalf("expected pat-seed deleted, got %v", err)
	}
}

func TestUnsupportedBundleTypeOrMethod(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()

	batchBundle := []byte(`{"resourceType":"Bundle","type":"batch","entry":[{"request":{"method":"GET","url":"Patient"}}]}`)
	_, err := harness.svc.ProcessTransactionBundle(ctx, &types.ResourceEnvelope{JSON: batchBundle})
	if err == nil || core.KindOf(err) != core.ErrorKindNotSupported {
		t.Fatalf("expected not-supported for batch, got %v", err)
	}

	patchBundle := []byte(`{"resourceType":"Bundle","type":"transaction","entry":[{"request":{"method":"PATCH","url":"Patient/p1"}}]}`)
	_, err = harness.svc.ProcessTransactionBundle(ctx, &types.ResourceEnvelope{JSON: patchBundle})
	if err == nil || core.KindOf(err) != core.ErrorKindNotSupported {
		t.Fatalf("expected not-supported for PATCH, got %v", err)
	}

	getBundle := []byte(`{"resourceType":"Bundle","type":"transaction","entry":[{"request":{"method":"GET","url":"Patient/p1"}}]}`)
	_, err = harness.svc.ProcessTransactionBundle(ctx, &types.ResourceEnvelope{JSON: getBundle})
	if err == nil || core.KindOf(err) != core.ErrorKindNotSupported {
		t.Fatalf("expected not-supported for GET, got %v", err)
	}

	condBundle := []byte(`{"resourceType":"Bundle","type":"transaction","entry":[{"request":{"method":"POST","url":"Patient?name=test"},"resource":{"resourceType":"Patient"}}]}`)
	_, err = harness.svc.ProcessTransactionBundle(ctx, &types.ResourceEnvelope{JSON: condBundle})
	if err == nil || core.KindOf(err) != core.ErrorKindNotSupported {
		t.Fatalf("expected not-supported for conditional URL, got %v", err)
	}
}

func TestOptionalCollaboratorsOmitted(t *testing.T) {
	harness := newTestHarness(t, harnessOptions{})
	ctx := context.Background()

	created, err := harness.svc.Create(ctx, patientEnvelope("pat-1", "Doe"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Hash == "" {
		t.Fatal("expected hash on create")
	}
}

func TestOperationOutcomeFromError(t *testing.T) {
	cases := []struct {
		kind core.ErrorKind
		code string
	}{
		{core.ErrorKindInvalid, "invalid"},
		{core.ErrorKindConflict, "conflict"},
		{core.ErrorKindNotFound, "not-found"},
		{core.ErrorKindNotSupported, "not-supported"},
		{core.ErrorKindException, "exception"},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			outcome := core.OperationOutcomeFromError(&core.ServiceError{
				Kind:       tc.kind,
				Message:    "test message",
				Expression: []string{"Resource.id"},
			})
			if len(outcome.Issue) != 1 {
				t.Fatalf("issue count = %d", len(outcome.Issue))
			}
			if outcome.Issue[0].Code != tc.code {
				t.Fatalf("code = %q, want %q", outcome.Issue[0].Code, tc.code)
			}
			if len(outcome.Issue[0].Expression) != 1 || outcome.Issue[0].Expression[0] != "Resource.id" {
				t.Fatalf("expression = %v", outcome.Issue[0].Expression)
			}
		})
	}
}

func TestOperationOutcomeFromErrorPreservesSDCValidationIssues(t *testing.T) {
	err := sdc.ValidationError{
		Outcome: sdc.Outcome{
			ResourceType: "OperationOutcome",
			Issue: []sdc.Issue{
				{Severity: "error", Code: "required", Diagnostics: "first", FieldPath: "item[a]"},
				{Severity: "error", Code: "type", Diagnostics: "second", FieldPath: "item[b]"},
			},
		},
	}
	outcome := core.OperationOutcomeFromError(err)
	if len(outcome.Issue) != 2 {
		t.Fatalf("issue count = %d, want 2", len(outcome.Issue))
	}
	if outcome.Issue[0].Code != "required" || outcome.Issue[1].Code != "type" {
		t.Fatalf("codes = %v, %v", outcome.Issue[0].Code, outcome.Issue[1].Code)
	}
	if core.KindOf(err) != core.ErrorKindInvalid {
		t.Fatalf("KindOf = %v, want invalid", core.KindOf(err))
	}
}

func patientEnvelope(id, family string) *types.ResourceEnvelope {
	payload := map[string]interface{}{
		"resourceType": "Patient",
		"name":         []map[string]string{{"family": family}},
	}
	if id != "" {
		payload["id"] = id
	}
	data, _ := json.Marshal(payload)
	return &types.ResourceEnvelope{ResourceType: "Patient", JSON: data}
}

type harnessOptions struct {
	outbox    bool
	indexer   bool
	validator validate.Validator
}

type testHarness struct {
	svc *core.ResourceService
	mem *memBackend
}

func newTestHarness(t *testing.T, opts harnessOptions) *testHarness {
	t.Helper()
	mem := newMemBackend()
	cfg := core.ResourceServiceConfig{
		Resources: mem,
		History:   mem,
		Sessions:  mem,
		IDPolicy:  core.DefaultIDPolicy{},
	}
	if opts.validator != nil {
		cfg.Validator = opts.validator
	}
	if opts.indexer {
		cfg.Indexer = &staticIndexer{}
	}
	if opts.outbox {
		cfg.Outbox = &hasync.EventStoreOutbox{}
	}
	svc, err := core.NewResourceService(cfg)
	if err != nil {
		t.Fatalf("NewResourceService: %v", err)
	}
	return &testHarness{svc: svc, mem: mem}
}

type failValidator struct {
	err error
}

func (v *failValidator) ValidateResource(context.Context, *types.ResourceEnvelope) error {
	return v.err
}

type failIndexer struct {
	err error
}

func (i *failIndexer) Build(context.Context, *types.ResourceEnvelope) ([]store.SearchIndexEntry, error) {
	return nil, i.err
}

type staticIndexer struct{}

func (i *staticIndexer) Build(_ context.Context, resource *types.ResourceEnvelope) ([]store.SearchIndexEntry, error) {
	return []store.SearchIndexEntry{{
		ResourceType: resource.ResourceType,
		ID:           resource.ID,
		Fields:       map[string]string{"string.family": "indexed"},
	}}, nil
}

type memBackend struct {
	mu        sync.Mutex
	resources map[string]*types.ResourceEnvelope
	history   map[string][]store.ResourceVersion
	search    map[string]store.SearchIndexEntry
	events    []store.ResourceEvent
}

func newMemBackend() *memBackend {
	return &memBackend{
		resources: make(map[string]*types.ResourceEnvelope),
		history:   make(map[string][]store.ResourceVersion),
		search:    make(map[string]store.SearchIndexEntry),
	}
}

func (m *memBackend) key(resourceType, id string) string {
	return resourceType + "/" + id
}

func (m *memBackend) resourceCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.resources)
}

func (m *memBackend) Create(_ context.Context, res *types.ResourceEnvelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(res.ResourceType, res.ID)
	if _, ok := m.resources[k]; ok {
		return fmt.Errorf("resource already exists: %s/%s", res.ResourceType, res.ID)
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
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	cp := *res
	return &cp, nil
}

func (m *memBackend) Update(_ context.Context, res *types.ResourceEnvelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(res.ResourceType, res.ID)
	if _, ok := m.resources[k]; !ok {
		return fmt.Errorf("resource not found: %s/%s", res.ResourceType, res.ID)
	}
	cp := *res
	m.resources[k] = &cp
	return nil
}

func (m *memBackend) Delete(_ context.Context, resourceType, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(resourceType, id)
	if _, ok := m.resources[k]; !ok {
		return fmt.Errorf("resource not found: %s/%s", resourceType, id)
	}
	delete(m.resources, k)
	return nil
}

func (m *memBackend) Exists(_ context.Context, resourceType, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.resources[m.key(resourceType, id)]
	return ok, nil
}

func (m *memBackend) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for key, res := range m.resources {
		if res.ResourceType != resourceType {
			continue
		}
		ids = append(ids, res.ID)
		_ = key
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

func (m *memBackend) AppendVersion(_ context.Context, version store.ResourceVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(version.ResourceType, version.ID)
	m.history[k] = append(m.history[k], version)
	return nil
}

func (m *memBackend) GetHistory(_ context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(resourceType, id)
	out := append([]store.ResourceVersion(nil), m.history[k]...)
	return out, nil
}

func (m *memBackend) BeginWrite(ctx context.Context) (store.WriteSession, error) {
	s := &memSession{
		backend: m,
		snapshot: memSnapshot{
			resources: cloneResourceMap(m.resources),
			history:   cloneHistoryMap(m.history),
			search:    cloneSearchMap(m.search),
			events:    append([]store.ResourceEvent(nil), m.events...),
		},
	}
	return s, nil
}

type memSnapshot struct {
	resources map[string]*types.ResourceEnvelope
	history   map[string][]store.ResourceVersion
	search    map[string]store.SearchIndexEntry
	events    []store.ResourceEvent
}

type memSession struct {
	backend  *memBackend
	snapshot memSnapshot
	events   memEventStore
}

func (s *memSession) ResourceStore() store.ResourceStore { return s.backend }
func (s *memSession) HistoryStore() store.HistoryStore   { return s.backend }
func (s *memSession) SearchStore() store.SearchStore     { return s.backend }
func (s *memSession) EventStore() store.EventStore       { return &s.events }

func (s *memSession) Commit(ctx context.Context) error {
	_ = ctx
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()
	s.backend.events = append(s.backend.events, s.events.events...)
	return nil
}

func (s *memSession) Rollback(ctx context.Context) error {
	_ = ctx
	m := s.backend
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = cloneResourceMap(s.snapshot.resources)
	m.history = cloneHistoryMap(s.snapshot.history)
	m.search = cloneSearchMap(s.snapshot.search)
	m.events = append([]store.ResourceEvent(nil), s.snapshot.events...)
	return nil
}

type memEventStore struct {
	events []store.ResourceEvent
}

func (e *memEventStore) Append(_ context.Context, event store.ResourceEvent) (store.ResourceEvent, error) {
	event.Sequence = int64(len(e.events) + 1)
	e.events = append(e.events, event)
	return event, nil
}

func (e *memEventStore) ReadSince(_ context.Context, afterSequence int64, limit int) ([]store.ResourceEvent, error) {
	var out []store.ResourceEvent
	for _, ev := range e.events {
		if ev.Sequence > afterSequence {
			out = append(out, ev)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (e *memEventStore) LatestForResource(_ context.Context, resourceType, id string) (*store.ResourceEvent, error) {
	for i := len(e.events) - 1; i >= 0; i-- {
		ev := e.events[i]
		if ev.ResourceType == resourceType && ev.ID == id {
			copy := ev
			return &copy, nil
		}
	}
	return nil, nil
}

func (m *memBackend) Index(_ context.Context, entry store.SearchIndexEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.search[m.key(entry.ResourceType, entry.ID)] = entry
	return nil
}

func (m *memBackend) RemoveIndex(_ context.Context, resourceType, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.search, m.key(resourceType, id))
	return nil
}

func (m *memBackend) Lookup(_ context.Context, key, value string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for k, entry := range m.search {
		if entry.Fields[key] == value {
			ids = append(ids, strings.TrimPrefix(k, entry.ResourceType+"/"))
		}
	}
	return ids, nil
}

func (m *memBackend) QueryPrepared(context.Context, store.PreparedQuery, map[string]string) ([]string, error) {
	return nil, nil
}

func cloneResourceMap(src map[string]*types.ResourceEnvelope) map[string]*types.ResourceEnvelope {
	out := make(map[string]*types.ResourceEnvelope, len(src))
	for k, v := range src {
		cp := *v
		out[k] = &cp
	}
	return out
}

func cloneHistoryMap(src map[string][]store.ResourceVersion) map[string][]store.ResourceVersion {
	out := make(map[string][]store.ResourceVersion, len(src))
	for k, v := range src {
		out[k] = append([]store.ResourceVersion(nil), v...)
	}
	return out
}

func cloneSearchMap(src map[string]store.SearchIndexEntry) map[string]store.SearchIndexEntry {
	out := make(map[string]store.SearchIndexEntry, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

var (
	_ store.ResourceStore        = (*memBackend)(nil)
	_ store.HistoryStore         = (*memBackend)(nil)
	_ store.SearchStore          = (*memBackend)(nil)
	_ store.WriteSessionProvider = (*memBackend)(nil)
	_ validate.Validator         = (*failValidator)(nil)
	_ search.Indexer             = (*staticIndexer)(nil)
)
