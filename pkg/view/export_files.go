package view

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ExportFileStore persists view export artifacts keyed by job id and filename.
type ExportFileStore interface {
	Put(ctx context.Context, jobID, filename string, data []byte, contentType string) error
	Get(ctx context.Context, jobID, filename string) ([]byte, string, error)
	DeleteJob(ctx context.Context, jobID string) error
}

type inMemoryExportFileStore struct {
	mu    sync.RWMutex
	files map[string]storedExportFile
}

type storedExportFile struct {
	data        []byte
	contentType string
}

func exportFileKey(jobID, filename string) string {
	return jobID + "/" + filename
}

// NewInMemoryExportFileStore returns an in-memory ExportFileStore.
func NewInMemoryExportFileStore() ExportFileStore {
	return &inMemoryExportFileStore{files: make(map[string]storedExportFile)}
}

func (s *inMemoryExportFileStore) Put(_ context.Context, jobID, filename string, data []byte, contentType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[exportFileKey(jobID, filename)] = storedExportFile{
		data:        append([]byte(nil), data...),
		contentType: contentType,
	}
	return nil
}

func (s *inMemoryExportFileStore) Get(_ context.Context, jobID, filename string) ([]byte, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	file, ok := s.files[exportFileKey(jobID, filename)]
	if !ok {
		return nil, "", fmt.Errorf("view export file not found: %s/%s", jobID, filename)
	}
	return append([]byte(nil), file.data...), file.contentType, nil
}

func (s *inMemoryExportFileStore) DeleteJob(_ context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := jobID + "/"
	for key := range s.files {
		if strings.HasPrefix(key, prefix) {
			delete(s.files, key)
		}
	}
	return nil
}

// LocalExportFileStore persists view export artifacts on the local filesystem.
type LocalExportFileStore struct {
	root string
	mu   sync.Mutex
}

// NewLocalExportFileStore creates a filesystem-backed ExportFileStore.
func NewLocalExportFileStore(root string) (*LocalExportFileStore, error) {
	if root == "" {
		return nil, fmt.Errorf("view: export root path is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("view: create export root: %w", err)
	}
	return &LocalExportFileStore{root: root}, nil
}

func (s *LocalExportFileStore) filePath(jobID, filename string) string {
	return filepath.Join(s.root, filepath.FromSlash(jobID), filepath.FromSlash(filename))
}

func (s *LocalExportFileStore) metaPath(jobID, filename string) string {
	return s.filePath(jobID, filename) + ".meta"
}

func (s *LocalExportFileStore) Put(_ context.Context, jobID, filename string, data []byte, contentType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	full := s.filePath(jobID, filename)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(jobID, filename), []byte(contentType), 0o644)
}

func (s *LocalExportFileStore) Get(_ context.Context, jobID, filename string) ([]byte, string, error) {
	data, err := os.ReadFile(s.filePath(jobID, filename))
	if err != nil {
		return nil, "", fmt.Errorf("view export file not found: %s/%s", jobID, filename)
	}
	contentType := "application/octet-stream"
	if meta, err := os.ReadFile(s.metaPath(jobID, filename)); err == nil {
		contentType = strings.TrimSpace(string(meta))
	}
	return data, contentType, nil
}

func (s *LocalExportFileStore) DeleteJob(_ context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(filepath.Join(s.root, filepath.FromSlash(jobID)))
}

// FileExportWriter stores export artifacts in an ExportFileStore scoped to one job.
type FileExportWriter struct {
	store ExportFileStore
	jobID string
}

// NewFileExportWriter returns a writer that persists artifacts for one export job.
func NewFileExportWriter(store ExportFileStore, jobID string) ExportWriter {
	return &FileExportWriter{store: store, jobID: jobID}
}

func (w *FileExportWriter) WriteExport(ctx context.Context, filename string, result *Result, format OutputFormat) error {
	if w == nil || w.store == nil {
		return fmt.Errorf("view: export file store is required")
	}
	var buf trackedBuffer
	if err := writeFormattedExport(ctx, &buf, result, format); err != nil {
		return err
	}
	return w.store.Put(ctx, w.jobID, filename, buf.Bytes(), contentTypeForFormat(format))
}

type trackedBuffer struct {
	b []byte
}

func (t *trackedBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	return len(p), nil
}

func (t *trackedBuffer) Bytes() []byte {
	return t.b
}

func contentTypeForFormat(format OutputFormat) string {
	switch format {
	case FormatCSV:
		return "text/csv"
	case FormatParquet:
		return "application/octet-stream"
	case FormatJSON:
		return "application/json"
	default:
		return "application/fhir+ndjson"
	}
}
