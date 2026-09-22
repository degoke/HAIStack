package store

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// BlobObject is a stored blob payload or opaque backend reference.
// BinaryStore is intended for small inline payloads; BlobStore covers larger
// or externally referenced content without exposing object-storage SDK details.
type BlobObject struct {
	Key         string    `json:"key"`
	ContentType string    `json:"contentType,omitempty"`
	Size        int64     `json:"size"`
	Hash        string    `json:"hash,omitempty"`
	Data        []byte    `json:"data,omitempty"`
	Location    string    `json:"location,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// BlobStore stores and fetches blob payloads by stable key.
type BlobStore interface {
	Put(ctx context.Context, obj BlobObject) error
	Get(ctx context.Context, key string) (*BlobObject, error)
	Head(ctx context.Context, key string) (*BlobObject, error)
	Delete(ctx context.Context, key string) error
}

// BlobStoreWithStream optionally uploads blob payloads from an io.Reader
// without requiring the caller to materialize the full object as []byte.
// Size may be 0 for an empty object. Backends that require Content-Length
// (for example S3 PutObject) should reject a negative size.
type BlobStoreWithStream interface {
	BlobStore
	PutStream(ctx context.Context, key, contentType string, size int64, r io.Reader) error
}

// BlobStoreWithOpen optionally streams a blob payload. Head metadata is returned
// with Data omitted. Postgres hai_binary_object BYTEA still materializes in Open.
type BlobStoreWithOpen interface {
	BlobStore
	Open(ctx context.Context, key string) (io.ReadCloser, *BlobObject, error)
}

// PutBlob writes a blob. When blobs implements BlobStoreWithStream, the payload
// is streamed; otherwise PutBlob reads the full reader into memory and calls Put.
func PutBlob(ctx context.Context, blobs BlobStore, key, contentType string, size int64, r io.Reader) error {
	if blobs == nil {
		return fmt.Errorf("blob store is required")
	}
	if r == nil {
		return fmt.Errorf("blob reader is required")
	}
	if streamer, ok := blobs.(BlobStoreWithStream); ok {
		return streamer.PutStream(ctx, key, contentType, size, r)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if size <= 0 {
		size = int64(len(data))
	}
	return blobs.Put(ctx, BlobObject{
		Key:         key,
		ContentType: contentType,
		Size:        size,
		Data:        data,
	})
}

// PutBlobFromPath streams a file into BlobStore without the caller loading it
// via os.ReadFile. Backends that do not implement BlobStoreWithStream still
// buffer the file inside PutBlob.
func PutBlobFromPath(ctx context.Context, blobs BlobStore, key, contentType, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	return PutBlob(ctx, blobs, key, contentType, info.Size(), f)
}

// OpenBlob opens a blob for reading. When blobs implements BlobStoreWithOpen,
// the payload is streamed; otherwise OpenBlob calls Get and wraps Data.
func OpenBlob(ctx context.Context, blobs BlobStore, key string) (io.ReadCloser, *BlobObject, error) {
	if blobs == nil {
		return nil, nil, fmt.Errorf("blob store is required")
	}
	if opener, ok := blobs.(BlobStoreWithOpen); ok {
		return opener.Open(ctx, key)
	}
	obj, err := blobs.Get(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	if obj == nil {
		return nil, nil, fmt.Errorf("blob not found: %s", key)
	}
	head := *obj
	data := head.Data
	head.Data = nil
	return io.NopCloser(bytes.NewReader(data)), &head, nil
}
