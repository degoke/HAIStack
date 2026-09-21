package binary_test

import (
	"context"
	"errors"
	"fmt"
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
	} else if !errors.Is(err, binary.ErrNotFound) {
		t.Fatalf("Get after delete: %v", err)
	}
}

func TestAsStoreNil(t *testing.T) {
	if binary.AsStore(nil) != nil {
		t.Fatal("expected nil adapter")
	}
}

func TestFileObjectKey(t *testing.T) {
	got, err := binary.FileObjectKey("bulk-export", "job-1/Patient..ndjson")
	if err != nil {
		t.Fatalf("Patient..ndjson: %v", err)
	}
	if got != "bulk-export/job-1/Patient..ndjson" {
		t.Fatalf("key = %q", got)
	}
	if _, err := binary.FileObjectKey("bulk-export", "../escape"); err == nil {
		t.Fatal("expected traversal rejected")
	}
	if _, err := binary.FileObjectKey("bulk-export", "job/../Patient.ndjson"); err == nil {
		t.Fatal("expected parent segment rejected")
	}
}

func TestPrefixedFileStoreRejectsEmptyPayload(t *testing.T) {
	ctx := context.Background()
	blobs := &memStoreBlobs{data: map[string]store.BlobObject{
		"bulk-export/job/loc.ndjson": {
			Key:      "bulk-export/job/loc.ndjson",
			Location: "s3://bucket/key",
			Data:     nil,
		},
	}}
	files := binary.NewPrefixedFileStore(blobs, "bulk-export", "export", "application/fhir+ndjson")
	if _, _, err := files.Get(ctx, "job/loc.ndjson"); err == nil {
		t.Fatal("expected missing file for nil Data")
	}
	if err := files.Put(ctx, "job/Patient..ndjson", []byte("{}"), ""); err != nil {
		t.Fatalf("Put Patient..ndjson: %v", err)
	}
}

type memStoreBlobs struct {
	mu   sync.Mutex
	data map[string]store.BlobObject
}

func (s *memStoreBlobs) Put(_ context.Context, obj store.BlobObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = make(map[string]store.BlobObject)
	}
	s.data[obj.Key] = obj
	return nil
}

func (s *memStoreBlobs) Get(_ context.Context, key string) (*store.BlobObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.data[key]
	if !ok {
		return nil, fmt.Errorf("blob not found: %s", key)
	}
	copy := obj
	return &copy, nil
}

func (s *memStoreBlobs) Head(ctx context.Context, key string) (*store.BlobObject, error) {
	return s.Get(ctx, key)
}

func (s *memStoreBlobs) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}
