package bulkimport_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/bulkimport"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type memoryWriter struct {
	resources map[string]*types.ResourceEnvelope
	created   []*types.ResourceEnvelope
	updated   []*types.ResourceEnvelope
}

func (m *memoryWriter) key(resourceType, id string) string { return resourceType + "/" + id }

func cloneEnvelope(resource *types.ResourceEnvelope) *types.ResourceEnvelope {
	cp := *resource
	if resource.JSON != nil {
		cp.JSON = append([]byte(nil), resource.JSON...)
	}
	return &cp
}

func (m *memoryWriter) Create(_ context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	cp := cloneEnvelope(resource)
	m.created = append(m.created, cp)
	m.resources[m.key(resource.ResourceType, resource.ID)] = cp
	return cp, nil
}

func (m *memoryWriter) Update(_ context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	cp := cloneEnvelope(resource)
	m.updated = append(m.updated, cp)
	m.resources[m.key(resource.ResourceType, resource.ID)] = cp
	return cp, nil
}

func (m *memoryWriter) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	if env := m.resources[m.key(resourceType, id)]; env != nil {
		return env, nil
	}
	return nil, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "not found"}
}

func TestBulkImportRoundTrip(t *testing.T) {
	now := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	writer := &memoryWriter{resources: map[string]*types.ResourceEnvelope{}}
	files := bulkimport.NewInMemoryFileStore()
	jobs := bulkimport.NewInMemoryJobStore()
	svc, err := bulkimport.NewService(bulkimport.Config{
		Jobs:     jobs,
		Files:    files,
		Executor: &bulkimport.Executor{Resources: writer, Files: files},
		BasePath: "/fhir",
		Now:      func() time.Time { return now },
		NewID:    func() string { return "job-1" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	ndjson := []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}` + "\n" +
		`{"resourceType":"Patient","id":"p2","name":[{"family":"Roe"}]}`)
	job, err := svc.Kickoff(context.Background(), bulkimport.KickoffRequest{
		InputFormat: bulkimport.InputFormatNDJSON,
		Inputs:      []bulkimport.InputFile{{Type: "Patient", NDJSON: ndjson}},
		RequestURL:  "POST /fhir/$import",
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != bulkimport.StatusComplete {
		t.Fatalf("status = %s error=%s", job.Status, job.LastError)
	}
	manifest := svc.Manifest(job)
	if manifest == nil || len(manifest.Output) != 1 || manifest.Output[0].Count != 2 {
		t.Fatalf("manifest = %#v", manifest)
	}
	if _, err := writer.Read(context.Background(), "Patient", "p1"); err != nil {
		t.Fatalf("read p1: %v", err)
	}
	if _, err := writer.Read(context.Background(), "Patient", "p2"); err != nil {
		t.Fatalf("read p2: %v", err)
	}
}

func TestParseParametersKickoffInlineNDJSON(t *testing.T) {
	body := []byte(`{
		"resourceType":"Parameters",
		"parameter":[
			{"name":"inputFormat","valueCode":"application/fhir+ndjson"},
			{"name":"input","part":[
				{"name":"type","valueCode":"Patient"},
				{"name":"valueString","valueString":"{\"resourceType\":\"Patient\",\"id\":\"p1\"}"}
			]}
		]
	}`)
	req, err := bulkimport.ParseParametersKickoff(body)
	if err != nil {
		t.Fatalf("ParseParametersKickoff: %v", err)
	}
	if req.InputFormat != bulkimport.InputFormatNDJSON {
		t.Fatalf("format = %q", req.InputFormat)
	}
	if len(req.Inputs) != 1 || req.Inputs[0].Type != "Patient" {
		t.Fatalf("inputs = %#v", req.Inputs)
	}
	if !strings.Contains(string(req.Inputs[0].NDJSON), `"p1"`) {
		t.Fatalf("ndjson = %s", req.Inputs[0].NDJSON)
	}
}

func TestParseParametersKickoffRequiresInput(t *testing.T) {
	_, err := bulkimport.ParseParametersKickoff([]byte(`{"resourceType":"Parameters"}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestKickoffRejectsURLWithoutLoader(t *testing.T) {
	files := bulkimport.NewInMemoryFileStore()
	svc, err := bulkimport.NewService(bulkimport.Config{
		Jobs:     bulkimport.NewInMemoryJobStore(),
		Files:    files,
		Executor: &bulkimport.Executor{Resources: &memoryWriter{resources: map[string]*types.ResourceEnvelope{}}, Files: files},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_, err = svc.Kickoff(context.Background(), bulkimport.KickoffRequest{
		Inputs: []bulkimport.InputFile{{Type: "Patient", URL: "https://example.test/Patient.ndjson"}},
	})
	if err == nil {
		t.Fatal("expected url loader error")
	}
	if core.KindOf(err) != core.ErrorKindInvalid {
		t.Fatalf("kind = %s, want invalid", core.KindOf(err))
	}
}

func TestImportUpdatesExistingResource(t *testing.T) {
	writer := &memoryWriter{resources: map[string]*types.ResourceEnvelope{
		"Patient/p1": {ResourceType: "Patient", ID: "p1", JSON: []byte(`{"resourceType":"Patient","id":"p1"}`)},
	}}
	files := bulkimport.NewInMemoryFileStore()
	svc, err := bulkimport.NewService(bulkimport.Config{
		Jobs:     bulkimport.NewInMemoryJobStore(),
		Files:    files,
		Executor: &bulkimport.Executor{Resources: writer, Files: files},
		NewID:    func() string { return "job-upd" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	job, err := svc.Kickoff(context.Background(), bulkimport.KickoffRequest{
		Inputs: []bulkimport.InputFile{{
			Type:   "Patient",
			NDJSON: []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Updated"}]}`),
		}},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != bulkimport.StatusComplete {
		t.Fatalf("status = %s", job.Status)
	}
	got, err := writer.Read(context.Background(), "Patient", "p1")
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(got.JSON, &obj); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.JSON), "Updated") {
		t.Fatalf("json = %s", got.JSON)
	}
}

func TestImportStripsMetaVersionIDBeforePersist(t *testing.T) {
	writer := &memoryWriter{resources: map[string]*types.ResourceEnvelope{}}
	files := bulkimport.NewInMemoryFileStore()
	svc, err := bulkimport.NewService(bulkimport.Config{
		Jobs:     bulkimport.NewInMemoryJobStore(),
		Files:    files,
		Executor: &bulkimport.Executor{Resources: writer, Files: files},
		NewID:    func() string { return "job-meta" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ndjson := []byte(`{"resourceType":"Patient","id":"p1","meta":{"versionId":"9","lastUpdated":"2024-01-01T00:00:00Z","profile":["http://example.org/StructureDefinition/Patient"],"tag":[{"system":"http://example.org/tags","code":"imported"}]}}`)
	job, err := svc.Kickoff(context.Background(), bulkimport.KickoffRequest{
		Inputs: []bulkimport.InputFile{{Type: "Patient", NDJSON: ndjson}},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != bulkimport.StatusComplete {
		t.Fatalf("status = %s error=%s", job.Status, job.LastError)
	}
	if len(writer.created) != 1 {
		t.Fatalf("created = %d", len(writer.created))
	}
	got := writer.created[0]
	if got.VersionID != "" {
		t.Fatalf("envelope VersionID = %q, want empty", got.VersionID)
	}
	if !got.LastUpdated.IsZero() {
		t.Fatalf("envelope LastUpdated = %v, want zero", got.LastUpdated)
	}
	var obj map[string]any
	if err := json.Unmarshal(got.JSON, &obj); err != nil {
		t.Fatal(err)
	}
	meta, _ := obj["meta"].(map[string]any)
	if meta == nil {
		t.Fatalf("expected profile/tag meta to be preserved, json = %s", got.JSON)
	}
	if _, ok := meta["versionId"]; ok {
		t.Fatalf("versionId still present: %s", got.JSON)
	}
	if _, ok := meta["lastUpdated"]; ok {
		t.Fatalf("lastUpdated still present: %s", got.JSON)
	}
	if _, ok := meta["profile"]; !ok {
		t.Fatalf("profile dropped: %s", got.JSON)
	}
	if _, ok := meta["tag"]; !ok {
		t.Fatalf("tag dropped: %s", got.JSON)
	}
}

func TestKickoffClientErrorsAreInvalid(t *testing.T) {
	files := bulkimport.NewInMemoryFileStore()
	svc, err := bulkimport.NewService(bulkimport.Config{
		Jobs:     bulkimport.NewInMemoryJobStore(),
		Files:    files,
		Executor: &bulkimport.Executor{Resources: &memoryWriter{resources: map[string]*types.ResourceEnvelope{}}, Files: files},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_, err = svc.Kickoff(context.Background(), bulkimport.KickoffRequest{})
	if core.KindOf(err) != core.ErrorKindInvalid {
		t.Fatalf("empty inputs kind = %s", core.KindOf(err))
	}
	_, err = svc.Kickoff(context.Background(), bulkimport.KickoffRequest{
		InputFormat: "text/csv",
		Inputs:      []bulkimport.InputFile{{Type: "Patient", NDJSON: []byte(`{"resourceType":"Patient","id":"p1"}`)}},
	})
	if core.KindOf(err) != core.ErrorKindInvalid {
		t.Fatalf("format kind = %s", core.KindOf(err))
	}
}

type cancelOnCreateWriter struct {
	memoryWriter
	cancel func()
	once   bool
}

func (w *cancelOnCreateWriter) Create(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if w.cancel != nil && !w.once {
		w.once = true
		w.cancel()
	}
	return w.memoryWriter.Create(ctx, resource)
}

func TestRunJobDoesNotCompleteCancelledJob(t *testing.T) {
	writer := &cancelOnCreateWriter{memoryWriter: memoryWriter{resources: map[string]*types.ResourceEnvelope{}}}
	files := bulkimport.NewInMemoryFileStore()
	var svc *bulkimport.Service
	writer.cancel = func() {
		if err := svc.Cancel(context.Background(), "job-cancel"); err != nil {
			t.Errorf("Cancel: %v", err)
		}
	}
	var err error
	svc, err = bulkimport.NewService(bulkimport.Config{
		Jobs:     bulkimport.NewInMemoryJobStore(),
		Files:    files,
		Executor: &bulkimport.Executor{Resources: writer, Files: files},
		NewID:    func() string { return "job-cancel" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	job, err := svc.Kickoff(context.Background(), bulkimport.KickoffRequest{
		Inputs: []bulkimport.InputFile{{
			Type:   "Patient",
			NDJSON: []byte(`{"resourceType":"Patient","id":"p1"}`),
		}},
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != bulkimport.StatusCancelled {
		t.Fatalf("status = %s, want cancelled", job.Status)
	}
}
