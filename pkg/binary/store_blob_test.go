package binary_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

func (s *memBinaryBlobStore) PutStream(_ context.Context, blobID string, r io.Reader, size int64, contentType string) (*binary.BlobDescriptor, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	_ = size
	return s.Put(context.Background(), blobID, data, contentType)
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

func TestAsStorePutStream(t *testing.T) {
	inner := &memBinaryBlobStore{}
	blobs := binary.AsStore(inner)
	ctx := context.Background()
	payload := []byte("stream-me")
	if err := store.PutBlob(ctx, blobs, "k", "text/plain", int64(len(payload)), bytes.NewReader(payload)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	got, err := blobs.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Data) != "stream-me" {
		t.Fatalf("data = %q", got.Data)
	}
	rc, head, err := store.OpenBlob(ctx, blobs, "k")
	if err != nil {
		t.Fatalf("OpenBlob: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	openData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("OpenBlob read: %v", err)
	}
	if string(openData) != "stream-me" || head.Data != nil {
		t.Fatalf("open %q head.Data=%v", openData, head.Data)
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
	if _, err := binary.FileObjectKey("bulk-export", "job//Patient.ndjson"); err == nil {
		t.Fatal("expected empty segment rejected")
	}
}

func TestPrefixedFileStorePayloads(t *testing.T) {
	ctx := context.Background()
	payloadKey := "bulk-export/job/real.ndjson"
	blobs := &memStoreBlobs{data: map[string]store.BlobObject{
		payloadKey: {
			Key:  payloadKey,
			Data: []byte("hydrated"),
		},
		"bulk-export/job/ptr.ndjson": {
			Key:      "bulk-export/job/ptr.ndjson",
			Location: payloadKey,
			Data:     nil,
		},
		"bulk-export/job/s3ptr.ndjson": {
			Key:      "bulk-export/job/s3ptr.ndjson",
			Location: "s3://bucket/key",
			Data:     nil,
		},
		"bulk-export/job/empty.ndjson": {
			Key:  "bulk-export/job/empty.ndjson",
			Data: []byte{},
		},
	}}
	files := binary.NewPrefixedFileStore(blobs, "bulk-export", "export", "application/fhir+ndjson")
	if _, _, err := files.Get(ctx, "job/missing.ndjson"); err == nil || !errors.Is(err, binary.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	got, _, err := files.Get(ctx, "job/ptr.ndjson")
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if string(got) != "hydrated" {
		t.Fatalf("hydrated = %q", got)
	}
	_, _, err = files.Get(ctx, "job/s3ptr.ndjson")
	if err == nil || errors.Is(err, binary.ErrNotFound) {
		t.Fatalf("unresolved location should not look missing: %v", err)
	}
	empty, _, err := files.Get(ctx, "job/empty.ndjson")
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v", empty)
	}
	if err := files.Put(ctx, "job/Patient..ndjson", []byte("{}"), ""); err != nil {
		t.Fatalf("Put Patient..ndjson: %v", err)
	}
	if err := files.PutStream(ctx, "job/stream.ndjson", bytes.NewReader([]byte("streamed")), 8, "application/fhir+ndjson"); err != nil {
		t.Fatalf("PutStream: %v", err)
	}
	gotStream, _, err := files.Get(ctx, "job/stream.ndjson")
	if err != nil {
		t.Fatalf("Get stream: %v", err)
	}
	if string(gotStream) != "streamed" {
		t.Fatalf("stream = %q", gotStream)
	}
	rc, ct, err := files.Open(ctx, "job/stream.ndjson")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	openData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("Open read: %v", err)
	}
	if ct != "application/fhir+ndjson" || string(openData) != "streamed" {
		t.Fatalf("open %q %q", ct, openData)
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

func (s *memStoreBlobs) PutStream(_ context.Context, key, contentType string, size int64, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if size <= 0 {
		size = int64(len(data))
	}
	return s.Put(context.Background(), store.BlobObject{
		Key:         key,
		ContentType: contentType,
		Size:        size,
		Data:        data,
	})
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
