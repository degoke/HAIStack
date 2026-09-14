package export

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// JobStore persists bulk export job state.
type JobStore interface {
	Create(ctx context.Context, job Job) error
	Get(ctx context.Context, id string) (*Job, error)
	Update(ctx context.Context, job Job) error
}

// FileStore stores export artifact bytes keyed by relative path.
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
		return fmt.Errorf("export: job id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.ID]; exists {
		return fmt.Errorf("export: duplicate job %q", job.ID)
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
		return fmt.Errorf("export: job id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.ID]; !exists {
		return fmt.Errorf("export: job %q not found", job.ID)
	}
	s.jobs[job.ID] = job
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = storedFile{
		data:        append([]byte(nil), data...),
		contentType: contentType,
	}
	return nil
}

func (s *InMemoryFileStore) Get(_ context.Context, path string) ([]byte, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	file, ok := s.files[path]
	if !ok {
		return nil, "", fmt.Errorf("export: file %q not found", path)
	}
	return append([]byte(nil), file.data...), file.contentType, nil
}

func (s *InMemoryFileStore) Delete(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.files, path)
	return nil
}

// nowUTC returns the current UTC time; overridable in tests via Service.Now.
func nowUTC(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}
