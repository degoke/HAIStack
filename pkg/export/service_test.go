package export_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
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

	rc, _, err := svc.OpenFile(context.Background(), job.ID, "Patient.ndjson")
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	openData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("OpenFile read: %v", err)
	}
	if string(openData) != string(data) {
		t.Fatalf("OpenFile mismatch")
	}
}

func TestExecutorStreamsPutWithoutBufferedPut(t *testing.T) {
	now := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	resources := &memoryResources{byType: map[string]map[string]*types.ResourceEnvelope{
		"Patient": {
			"p1": {
				ResourceType: "Patient",
				ID:           "p1",
				LastUpdated:  now,
				JSON:         []byte(`{"resourceType":"Patient","id":"p1"}`),
			},
		},
	}}
	inner := export.NewInMemoryFileStore()
	files := &streamOnlyFileStore{inner: inner}
	executor := &export.Executor{Resources: resources, Files: files}
	_, err := executor.Execute(context.Background(), export.ExecuteRequest{
		JobID:         "job-stream",
		ResourceTypes: []string{"Patient"},
		BaseFileURL:   "/fhir/$export/files/job-stream",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if files.putCalls != 0 {
		t.Fatalf("putCalls=%d, want 0 (must stream via PutStream)", files.putCalls)
	}
	if files.putStreamCalls != 1 {
		t.Fatalf("putStreamCalls=%d, want 1", files.putStreamCalls)
	}
	got, _, err := inner.Get(context.Background(), "job-stream/Patient.ndjson")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(string(got), `"id":"p1"`) {
		t.Fatalf("payload = %s", got)
	}
}

type streamOnlyFileStore struct {
	inner          export.FileStoreWithStream
	mu             sync.Mutex
	putCalls       int
	putStreamCalls int
}

func (s *streamOnlyFileStore) Put(ctx context.Context, path string, data []byte, contentType string) error {
	s.mu.Lock()
	s.putCalls++
	s.mu.Unlock()
	if len(data) > 0 {
		return fmt.Errorf("buffered Put of %d bytes is not allowed", len(data))
	}
	return s.inner.Put(ctx, path, data, contentType)
}

func (s *streamOnlyFileStore) PutStream(ctx context.Context, path string, r io.Reader, size int64, contentType string) error {
	s.mu.Lock()
	s.putStreamCalls++
	s.mu.Unlock()
	return s.inner.PutStream(ctx, path, r, size, contentType)
}

func (s *streamOnlyFileStore) Get(ctx context.Context, path string) ([]byte, string, error) {
	return s.inner.Get(ctx, path)
}

func (s *streamOnlyFileStore) Open(ctx context.Context, path string) (io.ReadCloser, string, error) {
	return s.inner.Open(ctx, path)
}

func (s *streamOnlyFileStore) Delete(ctx context.Context, path string) error {
	return s.inner.Delete(ctx, path)
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
