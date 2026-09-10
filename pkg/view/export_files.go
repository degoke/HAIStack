package view

import (
	"context"
	"fmt"
	"sync"
)

// ExportFileStore persists view export artifacts keyed by job id and filename.
type ExportFileStore interface {
	Put(ctx context.Context, jobID, filename string, data []byte, contentType string) error
	Get(ctx context.Context, jobID, filename string) ([]byte, string, error)
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
