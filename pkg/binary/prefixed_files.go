package binary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
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
	return s.blobs.Put(ctx, store.BlobObject{
		Key:         key,
		ContentType: contentType,
		Size:        int64(len(data)),
		Data:        append([]byte(nil), data...),
		CreatedAt:   time.Now().UTC(),
	})
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
			return nil, "", fmt.Errorf("%s: file %q not found", fileStorePkg(s), path)
		}
		return nil, "", err
	}
	if obj == nil || obj.Data == nil {
		return nil, "", fmt.Errorf("%s: file %q not found", fileStorePkg(s), path)
	}
	ct := obj.ContentType
	if ct == "" {
		ct = s.defaultCT
	}
	return append([]byte(nil), obj.Data...), ct, nil
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

// FileObjectKey builds a blob key from prefix and a relative path.
// Path traversal segments ("." / "..") are rejected; names like Patient..ndjson are allowed.
func FileObjectKey(prefix, path string) (string, error) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" {
		return "", fmt.Errorf("%w: file path is required", ErrInvalidArgument)
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "." || part == ".." {
			return "", fmt.Errorf("%w: invalid file path %q", ErrInvalidArgument, path)
		}
	}
	return prefix + "/" + cleaned, nil
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
