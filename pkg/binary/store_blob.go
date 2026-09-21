package binary

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// AsStore adapts a BlobStore to store.BlobStore so callers that speak the
// persistence contract can use SQLite, S3, or other binary backends.
func AsStore(blobs BlobStore) store.BlobStore {
	if blobs == nil {
		return nil
	}
	return &storeAdapter{inner: blobs}
}

type storeAdapter struct {
	inner BlobStore
}

func (a *storeAdapter) Put(ctx context.Context, obj store.BlobObject) error {
	if obj.Key == "" {
		return fmt.Errorf("%w: key is required", ErrInvalidArgument)
	}
	_, err := a.inner.Put(ctx, obj.Key, obj.Data, obj.ContentType)
	return err
}

func (a *storeAdapter) PutStream(ctx context.Context, key, contentType string, size int64, r io.Reader) error {
	if key == "" {
		return fmt.Errorf("%w: key is required", ErrInvalidArgument)
	}
	if r == nil {
		return fmt.Errorf("%w: reader is required", ErrInvalidArgument)
	}
	if streamer, ok := a.inner.(BlobStoreWithStream); ok {
		_, err := streamer.PutStream(ctx, key, r, size, contentType)
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	_, err = a.inner.Put(ctx, key, data, contentType)
	return err
}

func (a *storeAdapter) Get(ctx context.Context, key string) (*store.BlobObject, error) {
	data, desc, err := a.inner.Get(ctx, key)
	if err != nil {
		return nil, mapStoreBlobError(key, err)
	}
	obj := &store.BlobObject{
		Key:  key,
		Data: append([]byte{}, data...),
		Size: int64(len(data)),
	}
	if desc != nil {
		obj.ContentType = desc.ContentType
		obj.Hash = desc.SHA256
		obj.Location = desc.Pointer.Ref
		if desc.Size > 0 {
			obj.Size = desc.Size
		}
	}
	return obj, nil
}

func (a *storeAdapter) Head(ctx context.Context, key string) (*store.BlobObject, error) {
	desc, err := a.inner.Head(ctx, key)
	if err != nil {
		return nil, mapStoreBlobError(key, err)
	}
	obj := &store.BlobObject{Key: key}
	if desc != nil {
		obj.ContentType = desc.ContentType
		obj.Size = desc.Size
		obj.Hash = desc.SHA256
		obj.Location = desc.Pointer.Ref
	}
	return obj, nil
}

func (a *storeAdapter) Delete(ctx context.Context, key string) error {
	if err := a.inner.Delete(ctx, key); err != nil {
		return mapStoreBlobError(key, err)
	}
	return nil
}

var _ store.BlobStoreWithStream = (*storeAdapter)(nil)

func mapStoreBlobError(key string, err error) error {
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("blob not found: %s: %w", key, ErrNotFound)
	}
	return err
}
