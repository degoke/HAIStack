package binary_test

import (
	"context"
	"sync"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/binary"
	"github.com/degoke/health-ai-stack/pkg/store"
)

type memBinaryBlobStore struct {
	mu   sync.Mutex
	data map[string]storedBlob
}

type storedBlob struct {
	data        []byte
	contentType string
}

func (s *memBinaryBlobStore) Put(_ context.Context, blobID string, data []byte, contentType string) (*binary.BlobDescriptor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = make(map[string]storedBlob)
	}
	s.data[blobID] = storedBlob{data: append([]byte(nil), data...), contentType: contentType}
	return &binary.BlobDescriptor{
		BlobID:      blobID,
		SHA256:      binary.HashSHA256(data),
		Size:        int64(len(data)),
		ContentType: contentType,
		Backend:     binary.BackendSQLite,
		Pointer:     binary.StoragePointer{Backend: binary.BackendSQLite, Ref: blobID},
	}, nil
}

func (s *memBinaryBlobStore) Get(_ context.Context, blobID string) ([]byte, *binary.BlobDescriptor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	blob, ok := s.data[blobID]
	if !ok {
		return nil, nil, binary.ErrNotFound
	}
	return append([]byte(nil), blob.data...), &binary.BlobDescriptor{
		BlobID:      blobID,
		Size:        int64(len(blob.data)),
		ContentType: blob.contentType,
		Pointer:     binary.StoragePointer{Ref: blobID},
	}, nil
}

func (s *memBinaryBlobStore) Head(_ context.Context, blobID string) (*binary.BlobDescriptor, error) {
	_, desc, err := s.Get(context.Background(), blobID)
	return desc, err
}

func (s *memBinaryBlobStore) Delete(_ context.Context, blobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[blobID]; !ok {
		return binary.ErrNotFound
	}
	delete(s.data, blobID)
	return nil
}

func TestAsStoreRoundTrip(t *testing.T) {
	inner := &memBinaryBlobStore{}
	blobs := binary.AsStore(inner)
	ctx := context.Background()
	if err := blobs.Put(ctx, store.BlobObject{
		Key:         "bulk-export/job/Patient.ndjson",
		ContentType: "application/fhir+ndjson",
		Data:        []byte("{\"resourceType\":\"Patient\"}\n"),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := blobs.Get(ctx, "bulk-export/job/Patient.ndjson")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Data) != "{\"resourceType\":\"Patient\"}\n" {
		t.Fatalf("data = %q", got.Data)
	}
	if got.ContentType != "application/fhir+ndjson" {
		t.Fatalf("content type = %q", got.ContentType)
	}
	head, err := blobs.Head(ctx, "bulk-export/job/Patient.ndjson")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if len(head.Data) != 0 {
		t.Fatalf("head included payload")
	}
	if err := blobs.Delete(ctx, "bulk-export/job/Patient.ndjson"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := blobs.Get(ctx, "bulk-export/job/Patient.ndjson"); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestAsStoreNil(t *testing.T) {
	if binary.AsStore(nil) != nil {
		t.Fatal("expected nil adapter")
	}
}
