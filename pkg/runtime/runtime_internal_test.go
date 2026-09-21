package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
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

type memRuntimeBlobs struct {
	store.BlobStore
}

func TestResolveBulkBlobStorePrefersObjectStoreAdapter(t *testing.T) {
	adapterBlobs := &memRuntimeBlobs{}
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
