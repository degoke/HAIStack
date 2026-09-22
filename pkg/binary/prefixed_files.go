package binary

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/degoke/haistack/pkg/store"
)

// PrefixedFileStore stores artifact bytes in a store.BlobStore under a key prefix.
type PrefixedFileStore struct {
	blobs     store.BlobStore
	prefix    string
	pkgName   string
	defaultCT string
}

// NewPrefixedFileStore wraps blobs for path-keyed files namespaced by prefix.
func NewPrefixedFileStore(blobs store.BlobStore, prefix, pkgName, defaultContentType string) *PrefixedFileStore {
	if defaultContentType == "" {
		defaultContentType = "application/fhir+ndjson"
	}
	if pkgName == "" {
		pkgName = "binary"
	}
	return &PrefixedFileStore{blobs: blobs, prefix: prefix, pkgName: pkgName, defaultCT: defaultContentType}
}

func (s *PrefixedFileStore) Put(ctx context.Context, path string, data []byte, contentType string) error {
	return s.PutStream(ctx, path, bytes.NewReader(data), int64(len(data)), contentType)
}

func (s *PrefixedFileStore) PutStream(ctx context.Context, path string, r io.Reader, size int64, contentType string) error {
	if s == nil || s.blobs == nil {
		return fmt.Errorf("%s: blob store is required", fileStorePkg(s))
	}
	key, err := FileObjectKey(s.prefix, path)
	if err != nil {
		return fmt.Errorf("%s: %w", fileStorePkg(s), err)
	}
	if contentType == "" {
		contentType = s.defaultCT
	}
	return store.PutBlob(ctx, s.blobs, key, contentType, size, r)
}

func (s *PrefixedFileStore) Get(ctx context.Context, path string) ([]byte, string, error) {
	if s == nil || s.blobs == nil {
		return nil, "", fmt.Errorf("%s: blob store is required", fileStorePkg(s))
	}
	key, err := FileObjectKey(s.prefix, path)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", fileStorePkg(s), err)
	}
	obj, err := s.blobs.Get(ctx, key)
	if err != nil {
		if IsBlobMissing(err) {
			return nil, "", fmt.Errorf("%s: file %q not found: %w", fileStorePkg(s), path, ErrNotFound)
		}
		return nil, "", err
	}
	if obj == nil {
		return nil, "", fmt.Errorf("%s: file %q not found: %w", fileStorePkg(s), path, ErrNotFound)
	}
	data, err := hydrateBlobPayload(ctx, s.blobs, obj, nil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, "", fmt.Errorf("%s: file %q not found: %w", fileStorePkg(s), path, ErrNotFound)
		}
		return nil, "", fmt.Errorf("%s: file %q: %w", fileStorePkg(s), path, err)
	}
	ct := obj.ContentType
	if ct == "" {
		ct = s.defaultCT
	}
	return data, ct, nil
}

func (s *PrefixedFileStore) Open(ctx context.Context, path string) (io.ReadCloser, string, error) {
	if s == nil || s.blobs == nil {
		return nil, "", fmt.Errorf("%s: blob store is required", fileStorePkg(s))
	}
	key, err := FileObjectKey(s.prefix, path)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", fileStorePkg(s), err)
	}
	rc, obj, err := store.OpenBlob(ctx, s.blobs, key)
	if err != nil {
		if IsBlobMissing(err) {
			return nil, "", fmt.Errorf("%s: file %q not found: %w", fileStorePkg(s), path, ErrNotFound)
		}
		return nil, "", err
	}
	if obj == nil {
		if rc != nil {
			_ = rc.Close()
		}
		return nil, "", fmt.Errorf("%s: file %q not found: %w", fileStorePkg(s), path, ErrNotFound)
	}
	// Open already returns the payload stream. Do not treat Location as another
	// blob key: AsStore copies Pointer.Ref there (chunk-store blob IDs, local
	// file paths, s3:// URIs). Pointer-only BYTEA rows are resolved in Open.
	ct := obj.ContentType
	if ct == "" {
		ct = s.defaultCT
	}
	return rc, ct, nil
}

func (s *PrefixedFileStore) Delete(ctx context.Context, path string) error {
	if s == nil || s.blobs == nil {
		return fmt.Errorf("%s: blob store is required", fileStorePkg(s))
	}
	key, err := FileObjectKey(s.prefix, path)
	if err != nil {
		return fmt.Errorf("%s: %w", fileStorePkg(s), err)
	}
	if err := s.blobs.Delete(ctx, key); err != nil && !IsBlobMissing(err) {
		return err
	}
	return nil
}

func fileStorePkg(s *PrefixedFileStore) string {
	if s == nil || s.pkgName == "" {
		return "binary"
	}
	return s.pkgName
}

// hydrateBlobPayload returns object bytes, following Location pointers once
// they resolve to another blob key. Empty Data is a valid empty file.
// Data==nil with no Location is missing. An unresolved Location is an error
// that is not ErrNotFound so callers do not treat a pointer-only object as 404.
func hydrateBlobPayload(ctx context.Context, blobs store.BlobStore, obj *store.BlobObject, seen map[string]struct{}) ([]byte, error) {
	if obj.Data != nil {
		return copyBytes(obj.Data), nil
	}
	if strings.TrimSpace(obj.Location) == "" {
		return nil, ErrNotFound
	}
	if blobs == nil {
		return nil, fmt.Errorf("blob %q has location %q but no payload", obj.Key, obj.Location)
	}
	if seen == nil {
		seen = make(map[string]struct{})
	}
	if _, ok := seen[obj.Location]; ok {
		return nil, fmt.Errorf("blob location cycle at %q", obj.Location)
	}
	seen[obj.Location] = struct{}{}
	next, err := blobs.Get(ctx, obj.Location)
	if err != nil {
		if IsBlobMissing(err) {
			return nil, fmt.Errorf("blob %q has location %q but no payload", obj.Key, obj.Location)
		}
		return nil, err
	}
	if next == nil {
		return nil, fmt.Errorf("blob %q has location %q but no payload", obj.Key, obj.Location)
	}
	if next.Data != nil {
		return copyBytes(next.Data), nil
	}
	if strings.TrimSpace(next.Location) == "" {
		return nil, fmt.Errorf("blob %q has location %q but no payload", obj.Key, obj.Location)
	}
	return hydrateBlobPayload(ctx, blobs, next, seen)
}

// FileObjectKey builds a blob key from prefix and a relative path.
// Path traversal segments (empty / "." / "..") are rejected; names like Patient..ndjson are allowed.
func FileObjectKey(prefix, path string) (string, error) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" {
		return "", fmt.Errorf("%w: file path is required", ErrInvalidArgument)
	}
	parts := strings.Split(cleaned, "/")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("%w: invalid file path %q", ErrInvalidArgument, path)
		}
		parts[i] = part
	}
	return prefix + "/" + strings.Join(parts, "/"), nil
}

// IsBlobMissing reports whether err means the blob key is absent.
func IsBlobMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) {
		return true
	}
	msg := err.Error()
	const token = "blob not found"
	return msg == token || strings.HasPrefix(msg, token+":") || strings.Contains(msg, ": "+token)
}
