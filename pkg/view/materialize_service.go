package view

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/google/uuid"
)

// MaterializeStatus tracks materialize job lifecycle.
type MaterializeStatus string

const (
	MaterializeInProgress MaterializeStatus = "in-progress"
	MaterializeComplete   MaterializeStatus = "complete"
	MaterializeError      MaterializeStatus = "error"
	MaterializeCancelled  MaterializeStatus = "cancelled"
)

// MaterializeRequest captures parameters for one materialize operation.
type MaterializeRequest struct {
	ViewName   string    `json:"viewName"`
	Version    string    `json:"version"`
	TargetName string    `json:"targetName,omitempty"`
	Since      time.Time `json:"since,omitempty"`
	Actor      string    `json:"actor,omitempty"`
}

// MaterializeJob is a durable materialize job record.
type MaterializeJob struct {
	ID          string              `json:"id"`
	Status      MaterializeStatus   `json:"status"`
	Request     MaterializeRequest  `json:"request"`
	RowCount    int                 `json:"rowCount,omitempty"`
	Progress    string              `json:"progress,omitempty"`
	LastError   string              `json:"lastError,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
	CompletedAt time.Time           `json:"completedAt,omitempty"`
	Cancelled   bool                `json:"cancelled,omitempty"`
}

// MaterializeResult is returned when a materialize job completes.
type MaterializeResult struct {
	ViewName   string            `json:"viewName"`
	Version    string            `json:"version"`
	TargetName string            `json:"targetName"`
	RowCount   int               `json:"rowCount"`
	Metadata   ResultMetadata    `json:"metadata"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

// MaterializeJobStore persists materialize jobs.
type MaterializeJobStore interface {
	Create(ctx context.Context, job MaterializeJob) error
	Get(ctx context.Context, id string) (*MaterializeJob, error)
	Update(ctx context.Context, job MaterializeJob) error
}

type inMemoryMaterializeJobStore struct {
	jobs map[string]MaterializeJob
}

// NewInMemoryMaterializeJobStore returns an ephemeral job store for tests and edge mode.
func NewInMemoryMaterializeJobStore() MaterializeJobStore {
	return &inMemoryMaterializeJobStore{jobs: make(map[string]MaterializeJob)}
}

func (s *inMemoryMaterializeJobStore) Create(_ context.Context, job MaterializeJob) error {
	s.jobs[job.ID] = job
	return nil
}

func (s *inMemoryMaterializeJobStore) Get(_ context.Context, id string) (*MaterializeJob, error) {
	job, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("materialize job not found: %s", id)
	}
	return &job, nil
}

func (s *inMemoryMaterializeJobStore) Update(_ context.Context, job MaterializeJob) error {
	if _, ok := s.jobs[job.ID]; !ok {
		return fmt.Errorf("materialize job not found: %s", job.ID)
	}
	s.jobs[job.ID] = job
	return nil
}

// MaterializeServiceConfig configures MaterializeService.
type MaterializeServiceConfig struct {
	Jobs      MaterializeJobStore
	Executor  *Executor
	JobQueue  store.JobStore
	BasePath  string
	Now       func() time.Time
	NewID     func() string
}

// MaterializeService orchestrates ViewDefinition $materialize operations.
type MaterializeService struct {
	jobs     MaterializeJobStore
	executor *Executor
	jobQueue store.JobStore
	basePath string
	now      func() time.Time
	newID    func() string
}

// NewMaterializeService validates configuration and returns a MaterializeService.
func NewMaterializeService(cfg MaterializeServiceConfig) (*MaterializeService, error) {
	if cfg.Jobs == nil {
		return nil, fmt.Errorf("view: materialize job store is required")
	}
	if cfg.Executor == nil {
		return nil, fmt.Errorf("view: executor is required")
	}
	if cfg.BasePath == "" {
		cfg.BasePath = "/fhir"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.NewID == nil {
		cfg.NewID = uuid.NewString
	}
	return &MaterializeService{
		jobs:     cfg.Jobs,
		executor: cfg.Executor,
		jobQueue: cfg.JobQueue,
		basePath: strings.TrimSuffix(cfg.BasePath, "/"),
		now:      cfg.Now,
		newID:    cfg.NewID,
	}, nil
}

// MaterializeJobPayload is the background job payload for materialize execution.
type MaterializeJobPayload struct {
	JobID string `json:"jobId"`
}

// Kickoff creates an async materialize job.
func (s *MaterializeService) Kickoff(ctx context.Context, req MaterializeRequest) (*MaterializeJob, error) {
	if s == nil {
		return nil, fmt.Errorf("view: materialize service is nil")
	}
	if req.ViewName == "" {
		return nil, fmt.Errorf("view: view name is required")
	}
	if s.executor.cfg.MaterializedViews == nil {
		return nil, ErrMissingMaterializedViewStore
	}
	id := s.newID()
	now := s.now()
	job := MaterializeJob{
		ID: id,
		Status: MaterializeInProgress,
		Request: req,
		CreatedAt: now,
		Progress: "0%",
	}
	if err := s.jobs.Create(ctx, job); err != nil {
		return nil, err
	}
	if s.jobQueue != nil {
		_, err := jobs.Enqueue(ctx, s.jobQueue, TypeViewMaterialize, MaterializeJobPayload{JobID: id}, jobs.EnqueueOptions{Now: s.now})
		if err != nil {
			return nil, err
		}
	} else if err := s.RunJob(ctx, id); err != nil {
		return nil, err
	}
	return s.jobs.Get(ctx, id)
}

// GetJob returns the current job state.
func (s *MaterializeService) GetJob(ctx context.Context, jobID string) (*MaterializeJob, error) {
	return s.jobs.Get(ctx, jobID)
}

// Cancel requests cancellation of an in-progress job.
func (s *MaterializeService) Cancel(ctx context.Context, jobID string) error {
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status != MaterializeInProgress {
		return fmt.Errorf("view: job %s is not in progress", jobID)
	}
	job.Cancelled = true
	job.Status = MaterializeCancelled
	job.CompletedAt = s.now()
	return s.jobs.Update(ctx, *job)
}

// StatusURL returns the polling URL for a job.
func (s *MaterializeService) StatusURL(jobID string) string {
	return fmt.Sprintf("%s/ViewDefinition/$materialize/status/%s", s.basePath, jobID)
}

// Result builds the completed operation result payload.
func (s *MaterializeService) Result(job *MaterializeJob) *MaterializeResult {
	if job == nil {
		return nil
	}
	target := job.Request.TargetName
	if target == "" {
		target = registryKey(job.Request.ViewName, job.Request.Version)
	}
	return &MaterializeResult{
		ViewName:   job.Request.ViewName,
		Version:    job.Request.Version,
		TargetName: target,
		RowCount:   job.RowCount,
		UpdatedAt:  job.CompletedAt,
	}
}

// JobHandler returns a jobs.Handler for async execution.
func (s *MaterializeService) JobHandler() jobs.Handler {
	return jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
		var payload MaterializeJobPayload
		if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
			return err
		}
		return s.RunJob(ctx, payload.JobID)
	})
}

// RunJob executes one materialize job synchronously.
func (s *MaterializeService) RunJob(ctx context.Context, jobID string) error {
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Cancelled {
		return nil
	}
	result, err := s.executor.Execute(ctx, ExecuteRequest{
		ViewName:    job.Request.ViewName,
		Version:     job.Request.Version,
		Actor:       job.Request.Actor,
		Since:       job.Request.Since,
		Materialize: true,
	})
	now := s.now()
	if err != nil {
		job.Status = MaterializeError
		job.LastError = err.Error()
		job.CompletedAt = now
		return s.jobs.Update(ctx, *job)
	}
	job.Status = MaterializeComplete
	job.RowCount = len(result.Rows)
	if result.Total > job.RowCount {
		job.RowCount = result.Total
	}
	job.Progress = "100%"
	job.CompletedAt = now
	return s.jobs.Update(ctx, *job)
}

// TypeViewMaterialize is the job type for ViewDefinition materialization.
const TypeViewMaterialize = jobs.TypeViewMaterialize
