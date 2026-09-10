package view

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// LocalViewExportJobStore persists ViewExportJob records as JSON files.
type LocalViewExportJobStore struct {
	dir string
	mu  sync.Mutex
}

// NewLocalViewExportJobStore creates a filesystem-backed ViewExportJobStore.
func NewLocalViewExportJobStore(dir string) (*LocalViewExportJobStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("view: export job store directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("view: create export job store: %w", err)
	}
	return &LocalViewExportJobStore{dir: dir}, nil
}

func (s *LocalViewExportJobStore) path(id string) string {
	return filepath.Join(s.dir, filepath.Clean(id)+".json")
}

func (s *LocalViewExportJobStore) Create(_ context.Context, job ViewExportJob) error {
	if job.ID == "" {
		return fmt.Errorf("view: export job id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.path(job.ID)); err == nil {
		return fmt.Errorf("view export job already exists: %s", job.ID)
	}
	return writeJSONFile(s.path(job.ID), job)
}

func (s *LocalViewExportJobStore) Get(_ context.Context, id string) (*ViewExportJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, fmt.Errorf("view export job not found: %s", id)
	}
	var job ViewExportJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("view: decode export job %s: %w", id, err)
	}
	return &job, nil
}

func (s *LocalViewExportJobStore) Update(_ context.Context, job ViewExportJob) error {
	if job.ID == "" {
		return fmt.Errorf("view: export job id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.path(job.ID)); err != nil {
		return fmt.Errorf("view export job not found: %s", job.ID)
	}
	return writeJSONFile(s.path(job.ID), job)
}

// LocalMaterializeJobStore persists MaterializeJob records as JSON files.
type LocalMaterializeJobStore struct {
	dir string
	mu  sync.Mutex
}

// NewLocalMaterializeJobStore creates a filesystem-backed MaterializeJobStore.
func NewLocalMaterializeJobStore(dir string) (*LocalMaterializeJobStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("view: materialize job store directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("view: create materialize job store: %w", err)
	}
	return &LocalMaterializeJobStore{dir: dir}, nil
}

func (s *LocalMaterializeJobStore) path(id string) string {
	return filepath.Join(s.dir, filepath.Clean(id)+".json")
}

func (s *LocalMaterializeJobStore) Create(_ context.Context, job MaterializeJob) error {
	if job.ID == "" {
		return fmt.Errorf("view: materialize job id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.path(job.ID)); err == nil {
		return fmt.Errorf("materialize job already exists: %s", job.ID)
	}
	return writeJSONFile(s.path(job.ID), job)
}

func (s *LocalMaterializeJobStore) Get(_ context.Context, id string) (*MaterializeJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, fmt.Errorf("materialize job not found: %s", id)
	}
	var job MaterializeJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("view: decode materialize job %s: %w", id, err)
	}
	return &job, nil
}

func (s *LocalMaterializeJobStore) Update(_ context.Context, job MaterializeJob) error {
	if job.ID == "" {
		return fmt.Errorf("view: materialize job id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.path(job.ID)); err != nil {
		return fmt.Errorf("materialize job not found: %s", job.ID)
	}
	return writeJSONFile(s.path(job.ID), job)
}

func writeJSONFile(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
