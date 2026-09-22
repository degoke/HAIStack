package view

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

// ExportFileStoreWithStream optionally writes artifacts from an io.Reader and
// opens them without requiring the caller to materialize the full payload as []byte.
type ExportFileStoreWithStream interface {
	ExportFileStore
	PutStream(ctx context.Context, jobID, filename string, r io.Reader, size int64, contentType string) error
	Open(ctx context.Context, jobID, filename string) (io.ReadCloser, string, error)
}

var (
	_ ExportFileStoreWithStream = (*inMemoryExportFileStore)(nil)
	_ ExportFileStoreWithStream = (*LocalExportFileStore)(nil)
)

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

// NewInMemoryExportFileStore returns an in-memory ExportFileStoreWithStream.
func NewInMemoryExportFileStore() ExportFileStoreWithStream {
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

// PutStream buffers r in memory. In-memory artifact stores are for tests and
// small payloads; LocalExportFileStore streams to disk without a full []byte.
func (s *inMemoryExportFileStore) PutStream(ctx context.Context, jobID, filename string, r io.Reader, size int64, contentType string) error {
	_ = size
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return s.Put(ctx, jobID, filename, data, contentType)
}

func (s *inMemoryExportFileStore) Open(ctx context.Context, jobID, filename string) (io.ReadCloser, string, error) {
	data, ct, err := s.Get(ctx, jobID, filename)
	if err != nil {
		return nil, "", err
	}
	return io.NopCloser(bytes.NewReader(data)), ct, nil
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

func (s *LocalExportFileStore) filePath(jobID, filename string) (string, error) {
	jobSeg, err := safeLocalPathSegment(jobID)
	if err != nil {
		return "", fmt.Errorf("view: invalid export job id: %w", err)
	}
	fileSeg, err := safeLocalPathSegment(filename)
	if err != nil {
		return "", fmt.Errorf("view: invalid export filename: %w", err)
	}
	full := filepath.Join(s.root, jobSeg, fileSeg)
	if err := ensurePathWithinRoot(s.root, full); err != nil {
		return "", err
	}
	return full, nil
}

func (s *LocalExportFileStore) metaPath(jobID, filename string) (string, error) {
	full, err := s.filePath(jobID, filename)
	if err != nil {
		return "", err
	}
	return full + ".meta", nil
}

func (s *LocalExportFileStore) Put(ctx context.Context, jobID, filename string, data []byte, contentType string) error {
	return s.PutStream(ctx, jobID, filename, bytes.NewReader(data), int64(len(data)), contentType)
}

func (s *LocalExportFileStore) PutStream(_ context.Context, jobID, filename string, r io.Reader, size int64, contentType string) error {
	_ = size
	s.mu.Lock()
	defer s.mu.Unlock()
	full, err := s.filePath(jobID, filename)
	if err != nil {
		return err
	}
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
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	meta, err := s.metaPath(jobID, filename)
	if err != nil {
		return err
	}
	return os.WriteFile(meta, []byte(contentType), 0o644)
}

func (s *LocalExportFileStore) Open(_ context.Context, jobID, filename string) (io.ReadCloser, string, error) {
	full, err := s.filePath(jobID, filename)
	if err != nil {
		return nil, "", err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, "", fmt.Errorf("view export file not found: %s/%s", jobID, filename)
	}
	contentType := "application/octet-stream"
	meta, err := s.metaPath(jobID, filename)
	if err != nil {
		_ = f.Close()
		return nil, "", err
	}
	if metaBytes, err := os.ReadFile(meta); err == nil {
		contentType = strings.TrimSpace(string(metaBytes))
	}
	return f, contentType, nil
}

func putExportFileFromPath(ctx context.Context, files ExportFileStore, jobID, filename, path, contentType string) error {
	if files == nil {
		return fmt.Errorf("view: export file store is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if streamer, ok := files.(ExportFileStoreWithStream); ok {
		return streamer.PutStream(ctx, jobID, filename, f, info.Size(), contentType)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	return files.Put(ctx, jobID, filename, data, contentType)
}

func (s *LocalExportFileStore) Get(ctx context.Context, jobID, filename string) ([]byte, string, error) {
	rc, ct, err := s.Open(ctx, jobID, filename)
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

func (s *LocalExportFileStore) DeleteJob(_ context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobSeg, err := safeLocalPathSegment(jobID)
	if err != nil {
		return fmt.Errorf("view: invalid export job id: %w", err)
	}
	dir := filepath.Join(s.root, jobSeg)
	if err := ensurePathWithinRoot(s.root, dir); err != nil {
		return err
	}
	return os.RemoveAll(dir)
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
		return ParquetContentType
	case FormatJSON:
		return "application/json"
	default:
		return "application/fhir+ndjson"
	}
}
