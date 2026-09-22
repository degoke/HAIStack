package export

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// LocalFileStore persists export artifacts on the local filesystem.
type LocalFileStore struct {
	root string
	mu   sync.Mutex
}

// NewLocalFileStore creates a filesystem-backed FileStore.
func NewLocalFileStore(root string) (*LocalFileStore, error) {
	if root == "" {
		return nil, fmt.Errorf("export: root path is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create export root: %w", err)
	}
	return &LocalFileStore{root: root}, nil
}

func (s *LocalFileStore) path(rel string) string {
	return filepath.Join(s.root, filepath.FromSlash(rel))
}

func (s *LocalFileStore) Put(ctx context.Context, path string, data []byte, contentType string) error {
	return s.PutStream(ctx, path, bytes.NewReader(data), int64(len(data)), contentType)
}

func (s *LocalFileStore) PutStream(_ context.Context, path string, r io.Reader, size int64, contentType string) error {
	_ = size
	_ = contentType
	s.mu.Lock()
	defer s.mu.Unlock()
	full := s.path(path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp := full + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, full)
}

func putFileFromPath(ctx context.Context, files FileStore, path, srcPath, contentType string) error {
	if files == nil {
		return fmt.Errorf("export: file store is required")
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if streamer, ok := files.(FileStoreWithStream); ok {
		return streamer.PutStream(ctx, path, f, info.Size(), contentType)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	return files.Put(ctx, path, data, contentType)
}

func (s *LocalFileStore) Get(ctx context.Context, path string) ([]byte, string, error) {
	rc, ct, err := s.Open(ctx, path)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", err
	}
	return data, ct, nil
}

func (s *LocalFileStore) Open(_ context.Context, path string) (io.ReadCloser, string, error) {
	f, err := os.Open(s.path(path))
	if err != nil {
		return nil, "", err
	}
	return f, "application/fhir+ndjson", nil
}

func (s *LocalFileStore) Delete(_ context.Context, path string) error {
	return os.Remove(s.path(path))
}

var _ FileStoreWithStream = (*LocalFileStore)(nil)
