package bulkimport_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/bulkimport"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
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
	store := bulkimport.NewDurableJobStore(db)
	job := bulkimport.Job{
		ID:        "job-1",
		Status:    bulkimport.StatusInProgress,
		Request:   bulkimport.KickoffRequest{InputFormat: bulkimport.InputFormatNDJSON, Inputs: []bulkimport.InputFile{{Type: "Patient"}}},
		CreatedAt: time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC),
		Progress:  "0%",
	}
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Get(ctx, "job-1")
	if err != nil || got == nil {
		t.Fatalf("Get: %v %#v", err, got)
	}
	if got.Request.InputFormat != bulkimport.InputFormatNDJSON || len(got.Request.Inputs) != 1 {
		t.Fatalf("got = %#v", got)
	}
	claimed, err := db.ClaimNext(ctx, jobs.TypeImportBulk)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed != nil {
		t.Fatalf("status record was claimed as worker job: %#v", claimed)
	}
	got.Status = bulkimport.StatusError
	got.LastError = "boom"
	if err := store.Update(ctx, *got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, err := store.Get(ctx, "job-1")
	if err != nil || updated == nil {
		t.Fatalf("Get after update: %v %#v", err, updated)
	}
	if updated.Status != bulkimport.StatusError || updated.LastError != "boom" {
		t.Fatalf("updated = %#v", updated)
	}
}

func TestDurableJobStoreUpdateDoesNotUncancel(t *testing.T) {
	ctx := context.Background()
	jobsStore := bulkimport.NewDurableJobStore(jobs.NewInMemoryJobStore())
	job := bulkimport.Job{ID: "job-1", Status: bulkimport.StatusInProgress}
	if err := jobsStore.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	cancelled := job
	cancelled.Status = bulkimport.StatusCancelled
	cancelled.CancelRequested = true
	if err := jobsStore.Update(ctx, cancelled); err != nil {
		t.Fatalf("Update cancelled: %v", err)
	}
	complete := job
	complete.Status = bulkimport.StatusComplete
	complete.Progress = "100%"
	if err := jobsStore.Update(ctx, complete); err != nil {
		t.Fatalf("Update complete: %v", err)
	}
	got, err := jobsStore.Get(ctx, job.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: %v %#v", err, got)
	}
	if got.Status != bulkimport.StatusCancelled || !got.CancelRequested {
		t.Fatalf("got = %#v", got)
	}
}

func TestBlobFileStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	blobs := &memBlobStore{}
	files := bulkimport.NewBlobFileStore(blobs)
	path := "job-1/input-0-Patient.ndjson"
	body := []byte("{\"resourceType\":\"Patient\",\"id\":\"p1\"}\n")
	if err := files.Put(ctx, path, body, bulkimport.InputFormatNDJSON); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := blobs.data["bulk-import/"+path]; !ok {
		t.Fatalf("expected blob key bulk-import/%s", path)
	}
	got, ct, err := files.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ct != bulkimport.InputFormatNDJSON || string(got) != string(body) {
		t.Fatalf("got %q %q", ct, got)
	}
}

func TestDurableStoresSurviveSQLiteReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "import-durable.db")
	open := func() (*sqlite.DB, bulkimport.JobStore, bulkimport.FileStore) {
		t.Helper()
		db, err := sqlite.Open(path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		return db, bulkimport.NewDurableJobStore(db.JobStore()), bulkimport.NewBlobFileStore(binary.AsStore(db.SQLiteBlobStore()))
	}

	db, jobStore, files := open()
	job := bulkimport.Job{
		ID:        "job-persist",
		Status:    bulkimport.StatusComplete,
		Request:   bulkimport.KickoffRequest{Inputs: []bulkimport.InputFile{{Type: "Patient"}}},
		CreatedAt: time.Now().UTC(),
		Output:    []bulkimport.CountFile{{Type: "Patient", Count: 1}},
	}
	if err := jobStore.Create(ctx, job); err != nil {
		t.Fatalf("Create: %v", err)
	}
	body := []byte("{\"resourceType\":\"OperationOutcome\"}\n")
	if err := files.Put(ctx, "job-persist/error-0-Patient.ndjson", body, bulkimport.InputFormatNDJSON); err != nil {
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
	if got.Status != bulkimport.StatusComplete || len(got.Output) != 1 {
		t.Fatalf("persisted job = %#v", got)
	}
	data, _, err := files.Get(ctx, "job-persist/error-0-Patient.ndjson")
	if err != nil {
		t.Fatalf("Get file after reopen: %v", err)
	}
	if string(data) != string(body) {
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
	jobsStore := bulkimport.NewDurableJobStore(errGetJobStore{
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
}
