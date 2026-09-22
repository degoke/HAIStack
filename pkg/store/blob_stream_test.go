package store_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/degoke/haistack/pkg/store"
)

type readSizeProbe struct {
	r       io.Reader
	maxRead int
}

func (p *readSizeProbe) Read(b []byte) (int, error) {
	if len(b) > p.maxRead {
		p.maxRead = len(b)
	}
	return p.r.Read(b)
}

type streamOnlyBlobStore struct {
	mu             sync.Mutex
	objects        map[string]store.BlobObject
	putCalls       int
	putStreamCalls int
	maxPutBytes    int
}

func newStreamOnlyBlobStore() *streamOnlyBlobStore {
	return &streamOnlyBlobStore{objects: make(map[string]store.BlobObject)}
}

func (s *streamOnlyBlobStore) Put(_ context.Context, obj store.BlobObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putCalls++
	if len(obj.Data) > s.maxPutBytes {
		s.maxPutBytes = len(obj.Data)
	}
	if len(obj.Data) > 0 {
		return fmt.Errorf("buffered Put of %d bytes is not allowed", len(obj.Data))
	}
	s.objects[obj.Key] = obj
	return nil
}

func (s *streamOnlyBlobStore) PutStream(_ context.Context, key, contentType string, size int64, r io.Reader) error {
	s.mu.Lock()
	s.putStreamCalls++
	s.mu.Unlock()
	w := &sliceWriter{}
	buf := make([]byte, 32*1024)
	if _, err := io.CopyBuffer(w, r, buf); err != nil {
		return err
	}
	if size <= 0 {
		size = int64(len(w.b))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = store.BlobObject{
		Key:         key,
		ContentType: contentType,
		Size:        size,
		Data:        w.b,
	}
	return nil
}

type sliceWriter struct {
	b []byte
}

func (w *sliceWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

func (s *streamOnlyBlobStore) Get(_ context.Context, key string) (*store.BlobObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("blob not found: %s", key)
	}
	copy := obj
	return &copy, nil
}

func (s *streamOnlyBlobStore) Head(ctx context.Context, key string) (*store.BlobObject, error) {
	return s.Get(ctx, key)
}

func (s *streamOnlyBlobStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *streamOnlyBlobStore) Open(ctx context.Context, key string) (io.ReadCloser, *store.BlobObject, error) {
	obj, err := s.Get(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	head := *obj
	data := head.Data
	head.Data = nil
	return io.NopCloser(bytes.NewReader(data)), &head, nil
}

type putOnlyBlobStore struct {
	obj store.BlobObject
}

func (s *putOnlyBlobStore) Put(_ context.Context, obj store.BlobObject) error {
	s.obj = obj
	return nil
}

func (s *putOnlyBlobStore) Get(context.Context, string) (*store.BlobObject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *putOnlyBlobStore) Head(context.Context, string) (*store.BlobObject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *putOnlyBlobStore) Delete(context.Context, string) error { return nil }

func TestPutBlobUsesStreamWhenAvailable(t *testing.T) {
	ctx := context.Background()
	blobs := newStreamOnlyBlobStore()
	payload := bytes.Repeat([]byte("a"), 256*1024)
	probe := &readSizeProbe{r: bytes.NewReader(payload)}
	if err := store.PutBlob(ctx, blobs, "k", "application/octet-stream", int64(len(payload)), probe); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	if blobs.putCalls != 0 {
		t.Fatalf("putCalls=%d, want 0", blobs.putCalls)
	}
	if blobs.putStreamCalls != 1 {
		t.Fatalf("putStreamCalls=%d, want 1", blobs.putStreamCalls)
	}
	if probe.maxRead >= len(payload) {
		t.Fatalf("max Read dest %d equals full payload; expected streaming copy buffer", probe.maxRead)
	}
	got, err := blobs.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got.Data, payload) {
		t.Fatalf("payload mismatch: %d bytes", len(got.Data))
	}
}

func TestPutBlobFallsBackToPut(t *testing.T) {
	ctx := context.Background()
	blobs := &putOnlyBlobStore{}
	if err := store.PutBlob(ctx, blobs, "k", "text/plain", 3, bytes.NewReader([]byte("abc"))); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	if string(blobs.obj.Data) != "abc" || blobs.obj.Size != 3 {
		t.Fatalf("obj=%+v", blobs.obj)
	}
}

func TestPutBlobFromPathDoesNotCallBufferedPut(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")
	payload := bytes.Repeat([]byte("b"), 256*1024)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	blobs := newStreamOnlyBlobStore()
	if err := store.PutBlobFromPath(ctx, blobs, "from-path", "application/octet-stream", path); err != nil {
		t.Fatalf("PutBlobFromPath: %v", err)
	}
	if blobs.putCalls != 0 {
		t.Fatalf("putCalls=%d, want 0 (caller must not os.ReadFile + Put)", blobs.putCalls)
	}
	if blobs.maxPutBytes != 0 {
		t.Fatalf("maxPutBytes=%d, want 0", blobs.maxPutBytes)
	}
	got, err := blobs.Get(ctx, "from-path")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got.Data, payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestOpenBlobOmitsDataAndStreams(t *testing.T) {
	ctx := context.Background()
	blobs := newStreamOnlyBlobStore()
	payload := []byte("open-me")
	if err := store.PutBlob(ctx, blobs, "k", "text/plain", int64(len(payload)), bytes.NewReader(payload)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	rc, head, err := store.OpenBlob(ctx, blobs, "k")
	if err != nil {
		t.Fatalf("OpenBlob: %v", err)
	}
	defer func() { _ = rc.Close() }()
	if head.Data != nil {
		t.Fatalf("head included payload")
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "open-me" {
		t.Fatalf("got %q", got)
	}
}
