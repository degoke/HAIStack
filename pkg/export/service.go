package export

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/google/uuid"
)

// Config configures a Service.
type Config struct {
	Jobs      JobStore
	Files     FileStore
	Executor  *Executor
	JobQueue  store.JobStore
	BasePath  string
	PublicURL string
	Now       func() time.Time
	NewID     func() string
}

// Service orchestrates bulk export kickoff, polling, cancellation, and execution.
type Service struct {
	jobs      JobStore
	files     FileStore
	executor  *Executor
	jobQueue  store.JobStore
	basePath  string
	publicURL string
	now       func() time.Time
	newID     func() string
}

// NewService validates configuration and returns a Service.
func NewService(cfg Config) (*Service, error) {
	if cfg.Jobs == nil {
		return nil, fmt.Errorf("export: JobStore is required")
	}
	if cfg.Files == nil {
		return nil, fmt.Errorf("export: FileStore is required")
	}
	if cfg.Executor == nil {
		return nil, fmt.Errorf("export: Executor is required")
	}
	if cfg.BasePath == "" {
		cfg.BasePath = "/fhir"
	}
	if cfg.PublicURL == "" {
		cfg.PublicURL = cfg.BasePath
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.NewID == nil {
		cfg.NewID = uuid.NewString
	}
	return &Service{
		jobs:      cfg.Jobs,
		files:     cfg.Files,
		executor:  cfg.Executor,
		jobQueue:  cfg.JobQueue,
		basePath:  strings.TrimSuffix(cfg.BasePath, "/"),
		publicURL: strings.TrimSuffix(cfg.PublicURL, "/"),
		now:       cfg.Now,
		newID:     cfg.NewID,
	}, nil
}

// Kickoff creates an async export job and optionally enqueues background execution.
func (s *Service) Kickoff(ctx context.Context, req KickoffRequest) (*Job, error) {
	if s == nil {
		return nil, fmt.Errorf("export: service is nil")
	}
	id := s.newID()
	now := nowUTC(s.now)
	job := Job{
		ID:        id,
		Status:    StatusInProgress,
		Request:   req,
		CreatedAt: now,
		Progress:  "0%",
	}
	if err := s.jobs.Create(ctx, job); err != nil {
		return nil, err
	}
	if s.jobQueue != nil {
		_, err := jobs.Enqueue(ctx, s.jobQueue, jobs.TypeExportBulk, JobPayload{JobID: id}, jobs.EnqueueOptions{
			Now: s.now,
		})
		if err != nil {
			return nil, err
		}
	} else if err := s.RunJob(ctx, id); err != nil {
		return nil, err
	}
	return s.jobs.Get(ctx, id)
}

// GetJob returns the current job state.
func (s *Service) GetJob(ctx context.Context, id string) (*Job, error) {
	return s.jobs.Get(ctx, id)
}

// Cancel requests cancellation of an in-progress export job.
func (s *Service) Cancel(ctx context.Context, id string) error {
	job, err := s.jobs.Get(ctx, id)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("export: job %q not found", id)
	}
	switch job.Status {
	case StatusComplete, StatusError, StatusCancelled:
		return nil
	}
	job.CancelRequested = true
	job.Status = StatusCancelled
	job.CompletedAt = nowUTC(s.now)
	return s.jobs.Update(ctx, *job)
}

// Manifest builds the export manifest for a completed job.
func (s *Service) Manifest(job *Job) *Manifest {
	if job == nil {
		return nil
	}
	return &Manifest{
		TransactionTime:     job.TransactionTime,
		Request:             job.Request.RequestURL,
		RequiresAccessToken: job.Request.RequiresAccess,
		Output:              append([]OutputFile(nil), job.Output...),
		Error:               append([]ErrorFile(nil), job.Errors...),
	}
}

// StatusURL returns the polling URL for a job.
func (s *Service) StatusURL(jobID string) string {
	return s.publicURL + "/$export/status/" + jobID
}

// FileURL returns the public URL prefix for exported files.
func (s *Service) FileURL(jobID string) string {
	return s.publicURL + "/$export/files/" + jobID
}

// GetFile serves one export artifact.
func (s *Service) GetFile(ctx context.Context, jobID, filename string) ([]byte, string, error) {
	path := jobID + "/" + strings.TrimPrefix(filename, "/")
	return s.files.Get(ctx, path)
}

// RunJob executes one export job synchronously.
func (s *Service) RunJob(ctx context.Context, jobID string) error {
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("export: job %q not found", jobID)
	}
	if job.Status != StatusInProgress {
		return nil
	}

	patientIDs, err := s.resolveGroupPatients(ctx, job.Request.GroupID)
	if err != nil {
		return s.failJob(ctx, job, err)
	}

	isCancelled := func() bool {
		current, getErr := s.jobs.Get(ctx, jobID)
		if getErr != nil || current == nil {
			return false
		}
		return current.CancelRequested || current.Status == StatusCancelled
	}

	result, err := s.executor.Execute(ctx, ExecuteRequest{
		JobID:         jobID,
		ResourceTypes: job.Request.ResourceTypes,
		Since:         job.Request.Since,
		GroupID:       job.Request.GroupID,
		PatientIDs:    patientIDs,
		BaseFileURL:   s.FileURL(jobID),
		IsCancelled:   isCancelled,
		OnProgress: func(done, total int) {
			if total <= 0 {
				return
			}
			current, getErr := s.jobs.Get(ctx, jobID)
			if getErr != nil || current == nil {
				return
			}
			current.Progress = fmt.Sprintf("%d%%", (done*100)/total)
			_ = s.jobs.Update(ctx, *current)
		},
	})
	if err != nil {
		if isCancelled() {
			job.Status = StatusCancelled
			job.CompletedAt = nowUTC(s.now)
			return s.jobs.Update(ctx, *job)
		}
		return s.failJob(ctx, job, err)
	}

	job.Status = StatusComplete
	job.TransactionTime = nowUTC(s.now)
	job.CompletedAt = job.TransactionTime
	job.Progress = "100%"
	job.Output = result.Output
	job.Errors = result.Errors
	return s.jobs.Update(ctx, *job)
}

func (s *Service) resolveGroupPatients(ctx context.Context, groupID string) ([]string, error) {
	if groupID == "" || s.executor == nil || s.executor.Resources == nil {
		return nil, nil
	}
	env, err := s.executor.Resources.Read(ctx, "Group", groupID)
	if err != nil {
		return nil, fmt.Errorf("read Group/%s: %w", groupID, err)
	}
	return ParseGroupPatientIDs(env)
}

func (s *Service) failJob(ctx context.Context, job *Job, cause error) error {
	job.Status = StatusError
	job.LastError = cause.Error()
	job.CompletedAt = nowUTC(s.now)
	return s.jobs.Update(ctx, *job)
}

// JobHandler returns a jobs.Handler that executes bulk export jobs.
func (s *Service) JobHandler() jobs.Handler {
	return jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
		var payload JobPayload
		if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
			return err
		}
		return s.RunJob(ctx, payload.JobID)
	})
}

// DeleteJobFiles removes export artifacts after download.
func (s *Service) DeleteJobFiles(ctx context.Context, jobID string) error {
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil || job == nil {
		return err
	}
	for _, out := range job.Output {
		_ = s.files.Delete(ctx, jobID+"/"+out.Type+".ndjson")
	}
	for _, out := range job.Errors {
		_ = s.files.Delete(ctx, jobID+"/"+strings.TrimSuffix(out.Type, ".error")+".error.ndjson")
	}
	return nil
}
