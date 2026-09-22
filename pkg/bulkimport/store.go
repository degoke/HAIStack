package bulkimport

import (
	"context"
	"fmt"
	"sync"

	"github.com/degoke/haistack/pkg/binary"
)

// JobStore persists bulk import job state.
type JobStore interface {
	Create(ctx context.Context, job Job) error
	Get(ctx context.Context, id string) (*Job, error)
	Update(ctx context.Context, job Job) error
}

// FileStore stores import input and error artifact bytes keyed by relative path.
type FileStore interface {
	Put(ctx context.Context, path string, data []byte, contentType string) error
	Get(ctx context.Context, path string) ([]byte, string, error)
	Delete(ctx context.Context, path string) error
}

// InMemoryJobStore is a concurrent-safe JobStore for tests and local use.
type InMemoryJobStore struct {
	mu   sync.RWMutex
	jobs map[string]Job
}

// NewInMemoryJobStore constructs an empty in-memory job store.
func NewInMemoryJobStore() *InMemoryJobStore {
	return &InMemoryJobStore{jobs: make(map[string]Job)}
}

func (s *InMemoryJobStore) Create(_ context.Context, job Job) error {
	if job.ID == "" {
		return fmt.Errorf("import: job id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.ID]; exists {
		return fmt.Errorf("import: duplicate job %q", job.ID)
	}
	s.jobs[job.ID] = job
	return nil
}

func (s *InMemoryJobStore) Get(_ context.Context, id string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, nil
	}
	copy := job
	return &copy, nil
}

func (s *InMemoryJobStore) Update(_ context.Context, job Job) error {
	if job.ID == "" {
		return fmt.Errorf("import: job id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, exists := s.jobs[job.ID]
	if !exists {
		return fmt.Errorf("import: job %q not found", job.ID)
	}
	// Cancellation wins over complete/in-progress writes that raced after a
	// stale Get. completeJob re-reads, but this store-level guard closes TOCTOU.
	s.jobs[job.ID] = applyCancelGuard(existing, job)
	return nil
}

func (s *InMemoryJobStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; !ok {
		return fmt.Errorf("import: job %q not found", id)
	}
	delete(s.jobs, id)
	return nil
}

// InMemoryFileStore is a concurrent-safe FileStore for tests and local use.
type InMemoryFileStore struct {
	mu    sync.RWMutex
	files map[string]storedFile
}

type storedFile struct {
	data        []byte
	contentType string
}

// NewInMemoryFileStore constructs an empty in-memory file store.
func NewInMemoryFileStore() *InMemoryFileStore {
	return &InMemoryFileStore{files: make(map[string]storedFile)}
}

func (s *InMemoryFileStore) Put(_ context.Context, path string, data []byte, contentType string) error {
	if path == "" {
		return fmt.Errorf("import: file path is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = storedFile{data: append([]byte(nil), data...), contentType: contentType}
	return nil
}

func (s *InMemoryFileStore) Get(_ context.Context, path string) ([]byte, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	file, ok := s.files[path]
	if !ok {
		return nil, "", fmt.Errorf("import: file %q not found: %w", path, binary.ErrNotFound)
	}
	return append([]byte(nil), file.data...), file.contentType, nil
}

func (s *InMemoryFileStore) Delete(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, path)
	return nil
}
