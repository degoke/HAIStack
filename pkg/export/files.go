package export

import (
	"context"
	"fmt"
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

func (s *LocalFileStore) Put(_ context.Context, path string, data []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	full := s.path(path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

func (s *LocalFileStore) Get(_ context.Context, path string) ([]byte, string, error) {
	data, err := os.ReadFile(s.path(path))
	if err != nil {
		return nil, "", err
	}
	return data, "application/fhir+ndjson", nil
}

func (s *LocalFileStore) Delete(_ context.Context, path string) error {
	return os.Remove(s.path(path))
}
