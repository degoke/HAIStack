package jobs

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// InMemoryJobStore is a concurrent-safe store.JobStore for tests and local
// development. Claim semantics match Postgres/SQLite: oldest eligible pending
// job, RunAfter gating, status=running, Attempts incremented on claim.
type InMemoryJobStore struct {
	mu   sync.Mutex
	jobs map[string]store.JobRecord
	Now  func() time.Time
}

// NewInMemoryJobStore constructs an empty in-memory job store.
func NewInMemoryJobStore() *InMemoryJobStore {
	return &InMemoryJobStore{jobs: make(map[string]store.JobRecord)}
}

var _ store.JobStore = (*InMemoryJobStore)(nil)

// Enqueue implements store.JobStore.
func (s *InMemoryJobStore) Enqueue(_ context.Context, job store.JobRecord) error {
	if job.ID == "" {
		return ErrEmptyJobID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jobs == nil {
		s.jobs = make(map[string]store.JobRecord)
	}
	if _, exists := s.jobs[job.ID]; exists {
		return ErrDuplicateJob
	}
	s.jobs[job.ID] = job
	return nil
}

// ClaimNext implements store.JobStore.
func (s *InMemoryJobStore) ClaimNext(_ context.Context, jobType string) (*store.JobRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	type candidate struct {
		id  string
		job store.JobRecord
	}
	var candidates []candidate
	for id, job := range s.jobs {
		if job.Type != jobType || job.Status != store.JobStatusPending {
			continue
		}
		if !job.RunAfter.IsZero() && job.RunAfter.After(now) {
			continue
		}
		candidates = append(candidates, candidate{id: id, job: job})
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i].job, candidates[j].job
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
	picked := candidates[0]
	job := picked.job
	job.Status = store.JobStatusRunning
	job.Attempts++
	job.UpdatedAt = now
	s.jobs[picked.id] = job
	copyJob := job
	return &copyJob, nil
}

// Update implements store.JobStore.
func (s *InMemoryJobStore) Update(_ context.Context, job store.JobRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[job.ID]; !ok {
		return ErrJobNotFound
	}
	s.jobs[job.ID] = job
	return nil
}

// Get implements store.JobStore.
func (s *InMemoryJobStore) Get(_ context.Context, id string) (*store.JobRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	copyJob := job
	return &copyJob, nil
}

func (s *InMemoryJobStore) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
