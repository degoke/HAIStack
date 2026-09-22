package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/degoke/haistack/pkg/export"
	"github.com/degoke/haistack/pkg/store"
)

func TestRuntimeShutdownReturnsBackgroundWorkerError(t *testing.T) {
	rt := &Runtime{}
	rt.recordBackgroundError(errors.Join(ErrBackgroundWorker, errors.New("background failure")))

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := rt.Shutdown(context.Background())
	if !errors.Is(err, ErrBackgroundWorker) {
		t.Fatalf("Shutdown err = %v, want ErrBackgroundWorker", err)
	}
}

func TestRuntimeShutdownCleansUpAfterCallerContextExpires(t *testing.T) {
	cleaned := false
	rt := &Runtime{}
	rt.cleanup.add(func() { cleaned = true })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rt.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !cleaned {
		t.Fatal("cleanup stack did not run after canceled shutdown context")
	}
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

type stubBlobAdapter struct {
	blobs store.BlobStore
}

func (s stubBlobAdapter) Name() string               { return "stub-object-store" }
func (s stubBlobAdapter) BlobStore() store.BlobStore { return s.blobs }

type adapterBlobStore struct {
	mu   sync.Mutex
	data map[string]store.BlobObject
}

func newAdapterBlobStore() *adapterBlobStore {
	return &adapterBlobStore{data: make(map[string]store.BlobObject)}
}

func (s *adapterBlobStore) Put(_ context.Context, obj store.BlobObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[obj.Key] = obj
	return nil
}

func (s *adapterBlobStore) Get(_ context.Context, key string) (*store.BlobObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("blob not found: %s", key)
	}
	copy := obj
	return &copy, nil
}

func (s *adapterBlobStore) Head(ctx context.Context, key string) (*store.BlobObject, error) {
	return s.Get(ctx, key)
}

func (s *adapterBlobStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func TestResolveBulkBlobStorePrefersObjectStoreAdapter(t *testing.T) {
	adapterBlobs := newAdapterBlobStore()
	b := &Builder{blobStore: stubBlobAdapter{blobs: adapterBlobs}}
	state := &wireState{services: &ServiceContainer{}}
	got := b.resolveBulkBlobStore(state)
	if got != store.BlobStore(adapterBlobs) {
		t.Fatalf("got %#v, want adapter blob store", got)
	}
}

func TestResolveBulkBlobStoreNilAdapterFallsThrough(t *testing.T) {
	b := &Builder{blobStore: stubBlobAdapter{}}
	state := &wireState{services: &ServiceContainer{}}
	if got := b.resolveBulkBlobStore(state); got != nil {
		t.Fatalf("got %#v, want nil without sqlite/postgres", got)
	}
}

func TestBulkFilesRoundTripThroughObjectStoreAdapter(t *testing.T) {
	ctx := context.Background()
	adapterBlobs := newAdapterBlobStore()
	b := &Builder{blobStore: stubBlobAdapter{blobs: adapterBlobs}}
	resolved := b.resolveBulkBlobStore(&wireState{services: &ServiceContainer{}})
	files := export.NewBlobFileStore(resolved)
	body := []byte("{\"resourceType\":\"Patient\",\"id\":\"p1\"}\n")
	if err := files.Put(ctx, "job-1/Patient.ndjson", body, "application/fhir+ndjson"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := adapterBlobs.data["bulk-export/job-1/Patient.ndjson"]; !ok {
		t.Fatalf("object store missing key, have %#v", adapterBlobs.data)
	}
	got, ct, err := files.Get(ctx, "job-1/Patient.ndjson")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ct != "application/fhir+ndjson" || string(got) != string(body) {
		t.Fatalf("got %q %q", ct, got)
	}
}
