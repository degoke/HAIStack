package export_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/export"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type memoryResources struct {
	byType map[string]map[string]*types.ResourceEnvelope
}

func (m *memoryResources) ListIDs(_ context.Context, resourceType string, limit, offset int) ([]string, error) {
	items := m.byType[resourceType]
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	if offset >= len(ids) {
		return nil, nil
	}
	end := offset + limit
	if end > len(ids) {
		end = len(ids)
	}
	return ids[offset:end], nil
}

func (m *memoryResources) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	if env := m.byType[resourceType][id]; env != nil {
		return env, nil
	}
	return nil, context.Canceled
}

func TestBulkExportRoundTrip(t *testing.T) {
	now := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	resources := &memoryResources{byType: map[string]map[string]*types.ResourceEnvelope{
		"Patient": {
			"p1": {
				ResourceType: "Patient",
				ID:           "p1",
				LastUpdated:  now,
				JSON:         []byte(`{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}`),
			},
		},
	}}
	files := export.NewInMemoryFileStore()
	jobs := export.NewInMemoryJobStore()
	executor := &export.Executor{Resources: resources, Files: files}
	svc, err := export.NewService(export.Config{
		Jobs:     jobs,
		Files:    files,
		Executor: executor,
		BasePath: "/fhir",
		Now:      func() time.Time { return now },
		NewID:    func() string { return "job-1" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	job, err := svc.Kickoff(context.Background(), export.KickoffRequest{
		ResourceTypes: []string{"Patient"},
		RequestURL:    "GET /fhir/$export?_type=Patient",
	})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != export.StatusComplete {
		t.Fatalf("status = %s", job.Status)
	}
	manifest := svc.Manifest(job)
	if manifest == nil || len(manifest.Output) != 1 {
		t.Fatalf("manifest output = %#v", manifest)
	}
	data, _, err := svc.GetFile(context.Background(), job.ID, "Patient.ndjson")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %d", len(lines))
	}
	var patient map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &patient); err != nil {
		t.Fatalf("unmarshal ndjson: %v", err)
	}
	if patient["id"] != "p1" {
		t.Fatalf("patient id = %v", patient["id"])
	}
}

func TestManifestSchema(t *testing.T) {
	now := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	manifest := export.Manifest{
		TransactionTime:     now,
		Request:             "GET /fhir/$export",
		RequiresAccessToken: true,
		Output:              []export.OutputFile{{Type: "Patient", URL: "http://example/fhir/$export/files/job-1/Patient.ndjson"}},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"transactionTime", "request", "requiresAccessToken", "output"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing manifest key %q", key)
		}
	}
}
