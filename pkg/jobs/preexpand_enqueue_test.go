package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

type trackingJobStore struct {
	jobs map[string]store.JobRecord
}

func (s *trackingJobStore) Enqueue(_ context.Context, job store.JobRecord) error {
	if s.jobs == nil {
		s.jobs = make(map[string]store.JobRecord)
	}
	if _, exists := s.jobs[job.ID]; exists {
		return ErrDuplicateJob
	}
	s.jobs[job.ID] = job
	return nil
}

func (s *trackingJobStore) ClaimNext(context.Context, string) (*store.JobRecord, error) {
	return nil, nil
}

func (s *trackingJobStore) Update(_ context.Context, job store.JobRecord) error {
	if s.jobs == nil {
		s.jobs = make(map[string]store.JobRecord)
	}
	s.jobs[job.ID] = job
	return nil
}

func (s *trackingJobStore) Get(_ context.Context, id string) (*store.JobRecord, error) {
	job, ok := s.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	copyJob := job
	return &copyJob, nil
}

func TestEnqueuePackPreExpandDedupesPendingAndResetsCompleted(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	st := &trackingJobStore{}
	enqueue := func() error {
		return EnqueuePackPreExpand(ctx, st, "__global__", "test-pack", "1.0", "tenant-a", func() time.Time { return now })
	}
	if err := enqueue(); err != nil {
		t.Fatal(err)
	}
	if len(st.jobs) != 1 {
		t.Fatalf("jobs=%d", len(st.jobs))
	}
	jobID := PackPreExpandJobID("__global__", "test-pack", "1.0")
	if err := enqueue(); err != nil {
		t.Fatal(err)
	}
	if len(st.jobs) != 1 {
		t.Fatalf("pending dedupe jobs=%d", len(st.jobs))
	}
	st.jobs[jobID] = store.JobRecord{ID: jobID, Type: TypeTerminologyPreExpand, Status: store.JobStatusCompleted}
	if err := enqueue(); err != nil {
		t.Fatal(err)
	}
	reset := st.jobs[jobID]
	if reset.Status != store.JobStatusPending || reset.Attempts != 0 {
		t.Fatalf("reset job=%+v", reset)
	}
}
