package search_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
)

type memJobStore struct {
	jobs []store.JobRecord
}

func (s *memJobStore) Enqueue(_ context.Context, job store.JobRecord) error {
	s.jobs = append(s.jobs, job)
	return nil
}

func (s *memJobStore) ClaimNext(_ context.Context, jobType string) (*store.JobRecord, error) {
	for i := range s.jobs {
		if s.jobs[i].Type != jobType || s.jobs[i].Status != store.JobStatusPending {
			continue
		}
		copyJob := s.jobs[i]
		copyJob.Status = store.JobStatusRunning
		copyJob.Attempts++
		s.jobs[i] = copyJob
		return &copyJob, nil
	}
	return nil, nil
}

func (s *memJobStore) Update(_ context.Context, job store.JobRecord) error {
	for i := range s.jobs {
		if s.jobs[i].ID == job.ID {
			s.jobs[i] = job
			return nil
		}
	}
	return errors.New("not found")
}

func (s *memJobStore) Get(_ context.Context, id string) (*store.JobRecord, error) {
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			copyJob := s.jobs[i]
			return &copyJob, nil
		}
	}
	return nil, errors.New("not found")
}

func TestReindexJobRunnerRunOnce(t *testing.T) {
	ctx := context.Background()
	jobs := &memJobStore{}
	worker := &search.ReindexWorker{
		Registry:  search.NewSnapshotRegistry(testSnapshot(t, "Patient")),
		Indexer:   mustIndexer(t, search.NewSnapshotRegistry(testSnapshot(t, "Patient"))),
		Resources: newMemResourceStore(),
		Search:    &memSearchBackend{},
	}
	runner := &search.ReindexJobRunner{Jobs: jobs, Worker: worker}

	id, err := search.EnqueueReindex(ctx, jobs, "Patient")
	if err != nil {
		t.Fatalf("EnqueueReindex: %v", err)
	}

	processed, err := runner.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !processed {
		t.Fatal("expected processed job")
	}

	job, err := jobs.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if job.Status != store.JobStatusCompleted {
		t.Fatalf("status = %q, want completed", job.Status)
	}
}

func TestReindexNotifierScheduleReindex(t *testing.T) {
	ctx := context.Background()
	jobs := &memJobStore{}
	notifier := search.NewReindexNotifier(jobs)

	if err := notifier.ScheduleReindex(ctx, "Patient", "Patient", "Observation"); err != nil {
		t.Fatalf("ScheduleReindex: %v", err)
	}
	if len(jobs.jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(jobs.jobs))
	}
	for _, job := range jobs.jobs {
		if job.Type != search.ReindexJobType {
			t.Fatalf("job type = %q", job.Type)
		}
		if job.Status != store.JobStatusPending {
			t.Fatalf("job status = %q", job.Status)
		}
		if job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() {
			t.Fatal("expected timestamps on enqueued job")
		}
	}
	_ = time.Now()
}
