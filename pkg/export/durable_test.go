package export_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/export"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type memBlobStore struct {
	mu   sync.Mutex
	data map[string]store.BlobObject
}

func (s *memBlobStore) Put(_ context.Context, obj store.BlobObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = make(map[string]store.BlobObject)
	}
	s.data[obj.Key] = obj
	return nil
}

func (s *memBlobStore) Get(_ context.Context, key string) (*store.BlobObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("blob not found: %s", key)
	}
	copy := obj
	return &copy, nil
}

func (s *memBlobStore) Head(ctx context.Context, key string) (*store.BlobObject, error) {
	obj, err := s.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	obj.Data = nil
	return obj, nil
}

func (s *memBlobStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return fmt.Errorf("blob not found: %s", key)
	}
	delete(s.data, key)
	return nil
}

func TestDurableJobStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := jobs.NewInMemoryJobStore()
	jobsStore := export.NewDurableJobStore(db)
	created := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	job := export.Job{
		ID:        "job-1",
		Status:    export.StatusInProgress,
		Request:   export.KickoffRequest{ResourceTypes: []string{"Patient"}, RequestURL: "GET /fhir/$export"},
		CreatedAt: created,
		Progress:  "0%",
	}
	if err := jobsStore.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := jobsStore.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil || got.Status != export.StatusInProgress || got.Request.RequestURL != job.Request.RequestURL {
		t.Fatalf("got = %#v", got)
	}
	record, err := db.Get(ctx, "job-1")
	if err != nil {
		t.Fatalf("db Get: %v", err)
	}
	if record.Type != jobs.TypeExportBulkRecord {
		t.Fatalf("type = %q", record.Type)
	}
	if record.Status != store.JobStatusRunning {
		t.Fatalf("store status = %q", record.Status)
	}
	claimed, err := db.ClaimNext(ctx, jobs.TypeExportBulk)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed != nil {
		t.Fatalf("status record was claimed as worker job: %#v", claimed)
	}
	got.Status = export.StatusComplete
	got.Progress = "100%"
	got.Output = []export.OutputFile{{Type: "Patient", URL: "/fhir/$export/files/job-1/Patient.ndjson"}}
	if err := jobsStore.Update(ctx, *got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, err := jobsStore.Get(ctx, "job-1")
	if err != nil || updated == nil {
		t.Fatalf("Get after update: %v %#v", err, updated)
	}
	if updated.Status != export.StatusComplete || len(updated.Output) != 1 {
		t.Fatalf("updated = %#v", updated)
	}
	missing, err := jobsStore.Get(ctx, "missing")
	if err != nil || missing != nil {
		t.Fatalf("missing Get = %#v %v", missing, err)
	}
}

func TestBlobFileStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	blobs := &memBlobStore{}
	files := export.NewBlobFileStore(blobs)
	path := "job-1/Patient.ndjson"
	body := []byte("{\"resourceType\":\"Patient\",\"id\":\"p1\"}\n")
	if err := files.Put(ctx, path, body, "application/fhir+ndjson"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := blobs.data["bulk-export/"+path]; !ok {
		t.Fatalf("expected blob key bulk-export/%s, got %#v", path, blobs.data)
	}
	got, ct, err := files.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ct != "application/fhir+ndjson" || string(got) != string(body) {
		t.Fatalf("got %q %q", ct, got)
	}
	if err := files.Delete(ctx, path); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := files.Get(ctx, path); err == nil {
		t.Fatal("expected missing file")
	}
	if err := files.Put(ctx, "../escape", body, ""); err == nil {
		t.Fatal("expected invalid path")
	}
	if err := files.Put(ctx, "job-1/Patient..ndjson", body, ""); err != nil {
		t.Fatalf("Patient..ndjson: %v", err)
	}
}

func TestDurableStoresSurviveSQLiteReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bulk-durable.db")
	open := func() (*sqlite.DB, export.JobStore, export.FileStore) {
		t.Helper()
		db, err := sqlite.Open(path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		return db, export.NewDurableJobStore(db.JobStore()), export.NewBlobFileStore(binary.AsStore(db.SQLiteBlobStore()))
	}

	db, jobStore, files := open()
	job := export.Job{
		ID:        "job-persist",
		Status:    export.StatusComplete,
		Request:   export.KickoffRequest{ResourceTypes: []string{"Patient"}},
		CreatedAt: time.Now().UTC(),
		Output:    []export.OutputFile{{Type: "Patient", URL: "/files/Patient.ndjson"}},
	}
	if err := jobStore.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	body := []byte("{\"resourceType\":\"Patient\"}\n")
	if err := files.Put(ctx, "job-persist/Patient.ndjson", body, "application/fhir+ndjson"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, jobStore, files = open()
	defer func() { _ = db.Close() }()
	got, err := jobStore.Get(ctx, "job-persist")
	if err != nil || got == nil {
		t.Fatalf("Get after reopen: %v %#v", err, got)
	}
	if got.Status != export.StatusComplete || len(got.Output) != 1 {
		t.Fatalf("persisted job = %#v", got)
	}
	data, _, err := files.Get(ctx, "job-persist/Patient.ndjson")
	if err != nil {
		t.Fatalf("Get file after reopen: %v", err)
	}
	if string(data) != string(body) {
		t.Fatalf("file = %q", data)
	}
}

func TestBulkExportWithDurableStores(t *testing.T) {
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
	blobs := &memBlobStore{}
	files := export.NewBlobFileStore(blobs)
	jobsStore := export.NewDurableJobStore(jobs.NewInMemoryJobStore())
	svc, err := export.NewService(export.Config{
		Jobs:     jobsStore,
		Files:    files,
		Executor: &export.Executor{Resources: resources, Files: files},
		BasePath: "/fhir",
		Now:      func() time.Time { return now },
		NewID:    func() string { return "job-durable" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	job, err := svc.Kickoff(context.Background(), export.KickoffRequest{ResourceTypes: []string{"Patient"}})
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if job.Status != export.StatusComplete {
		t.Fatalf("status = %s error=%s", job.Status, job.LastError)
	}
	data, _, err := svc.GetFile(context.Background(), job.ID, "Patient.ndjson")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if !strings.Contains(string(data), `"id":"p1"`) {
		t.Fatalf("file = %q", data)
	}
}

type errGetJobStore struct {
	inner store.JobStore
	err   error
}

func (s errGetJobStore) Enqueue(ctx context.Context, job store.JobRecord) error {
	return s.inner.Enqueue(ctx, job)
}
func (s errGetJobStore) ClaimNext(ctx context.Context, jobType string) (*store.JobRecord, error) {
	return s.inner.ClaimNext(ctx, jobType)
}
func (s errGetJobStore) Update(ctx context.Context, job store.JobRecord) error {
	return s.inner.Update(ctx, job)
}
func (s errGetJobStore) Get(ctx context.Context, id string) (*store.JobRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.inner.Get(ctx, id)
}

func TestDurableJobStoreGetPreservesStoreErrors(t *testing.T) {
	ctx := context.Background()
	jobsStore := export.NewDurableJobStore(errGetJobStore{
		inner: jobs.NewInMemoryJobStore(),
		err:   fmt.Errorf("connection refused"),
	})
	got, err := jobsStore.Get(ctx, "job-1")
	if err == nil || got != nil {
		t.Fatalf("Get = %#v %v", got, err)
	}
	if jobs.IsMissing(err) {
		t.Fatalf("connection error treated as missing: %v", err)
	}
	if err := jobsStore.Update(ctx, export.Job{ID: "job-1", Status: export.StatusComplete}); err == nil {
		t.Fatal("expected Update to return store error")
	} else if jobs.IsMissing(err) {
		t.Fatalf("Update treated failure as missing: %v", err)
	}
}

func TestDurableJobStoreUpdateDoesNotUncancel(t *testing.T) {
	ctx := context.Background()
	jobsStore := export.NewDurableJobStore(jobs.NewInMemoryJobStore())
	job := export.Job{ID: "job-1", Status: export.StatusInProgress}
	if err := jobsStore.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	cancelled := job
	cancelled.Status = export.StatusCancelled
	cancelled.CancelRequested = true
	if err := jobsStore.Update(ctx, cancelled); err != nil {
		t.Fatalf("Update cancelled: %v", err)
	}
	complete := job
	complete.Status = export.StatusComplete
	if err := jobsStore.Update(ctx, complete); err != nil {
		t.Fatalf("Update complete: %v", err)
	}
	got, err := jobsStore.Get(ctx, job.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: %v %#v", err, got)
	}
	if got.Status != export.StatusCancelled || !got.CancelRequested {
		t.Fatalf("got = %#v", got)
	}
}

type failNthEnqueue struct {
	*jobs.InMemoryJobStore
	n     int
	failN int
}

func (s *failNthEnqueue) Enqueue(ctx context.Context, job store.JobRecord) error {
	s.n++
	if s.n >= s.failN {
		return fmt.Errorf("queue full")
	}
	return s.InMemoryJobStore.Enqueue(ctx, job)
}

func TestKickoffMarksJobErrorWhenEnqueueFails(t *testing.T) {
	now := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	resources := &memoryResources{byType: map[string]map[string]*types.ResourceEnvelope{}}
	files := export.NewInMemoryFileStore()
	queue := &failNthEnqueue{InMemoryJobStore: jobs.NewInMemoryJobStore(), failN: 2}
	jobsStore := export.NewDurableJobStore(queue)
	svc, err := export.NewService(export.Config{
		Jobs:     jobsStore,
		Files:    files,
		Executor: &export.Executor{Resources: resources, Files: files},
		JobQueue: queue,
		BasePath: "/fhir",
		Now:      func() time.Time { return now },
		NewID:    func() string { return "job-1" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Kickoff(context.Background(), export.KickoffRequest{ResourceTypes: []string{"Patient"}}); err == nil {
		t.Fatal("expected Kickoff enqueue error")
	}
	got, err := jobsStore.Get(context.Background(), "job-1")
	if err != nil || got == nil {
		t.Fatalf("Get: %v %#v", err, got)
	}
	if got.Status != export.StatusError {
		t.Fatalf("status = %s, want error", got.Status)
	}
}
