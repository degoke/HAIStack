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
}

func (m *memoryWriter) key(resourceType, id string) string { return resourceType + "/" + id }

func (m *memoryWriter) Create(_ context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	cp := *resource
	m.resources[m.key(resource.ResourceType, resource.ID)] = &cp
	return &cp, nil
}

func (m *memoryWriter) Update(_ context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return m.Create(context.Background(), resource)
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
