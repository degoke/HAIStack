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

// WatermarkAdvancer tracks SQL-on-FHIR export watermarks.
type WatermarkAdvancer interface {
	Since(ctx context.Context, viewName, version string) (time.Time, error)
	Advance(ctx context.Context, viewName, version string, refreshedAt time.Time) error
}

// ExportStatus tracks view export job lifecycle.
type ExportStatus string

const (
	ExportInProgress ExportStatus = "in-progress"
	ExportComplete   ExportStatus = "complete"
	ExportError      ExportStatus = "error"
	ExportCancelled  ExportStatus = "cancelled"
)

// ViewExportRequest captures one ViewDefinition export operation.
type ViewExportRequest struct {
	Views  []ViewExportTarget
	Since  time.Time
	Format OutputFormat
	Actor  string
}

// ViewExportTarget identifies one view to export.
type ViewExportTarget struct {
	ViewName   string
	Version    string
	OutputName string
}

// ViewExportJob tracks async export progress.
type ViewExportJob struct {
	ID          string
	Status      ExportStatus
	Request     ViewExportRequest
	Files       []ExportFile
	Progress    string
	LastError   string
	CreatedAt   time.Time
	CompletedAt time.Time
	Cancelled   bool
}

// ExportFile describes one exported artifact.
type ExportFile struct {
	ViewName   string `json:"viewName"`
	Version    string `json:"version"`
	OutputName string `json:"outputName"`
	Filename   string `json:"filename"`
	RowCount   int    `json:"rowCount"`
	Format     string `json:"format"`
}

// ViewExportJobStore persists export jobs.
type ViewExportJobStore interface {
	Create(ctx context.Context, job ViewExportJob) error
	Get(ctx context.Context, id string) (*ViewExportJob, error)
	Update(ctx context.Context, job ViewExportJob) error
}

type inMemoryViewExportJobStore struct {
	jobs map[string]ViewExportJob
}

// NewInMemoryViewExportJobStore returns an ephemeral export job store.
func NewInMemoryViewExportJobStore() ViewExportJobStore {
	return &inMemoryViewExportJobStore{jobs: make(map[string]ViewExportJob)}
}

func (s *inMemoryViewExportJobStore) Create(_ context.Context, job ViewExportJob) error {
	s.jobs[job.ID] = job
	return nil
}

func (s *inMemoryViewExportJobStore) Get(_ context.Context, id string) (*ViewExportJob, error) {
	job, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("view export job not found: %s", id)
	}
	return &job, nil
}

func (s *inMemoryViewExportJobStore) Update(_ context.Context, job ViewExportJob) error {
	if _, ok := s.jobs[job.ID]; !ok {
		return fmt.Errorf("view export job not found: %s", job.ID)
	}
	s.jobs[job.ID] = job
	return nil
}

// ExportWriter receives one exported view artifact.
type ExportWriter interface {
	WriteExport(ctx context.Context, filename string, result *Result, format OutputFormat) error
}

// ExportServiceConfig configures ExportService.
type ExportServiceConfig struct {
	Jobs      ViewExportJobStore
	Executor  *Executor
	Writer    ExportWriter
	Watermark WatermarkAdvancer
	JobQueue  store.JobStore
	BasePath  string
	Now       func() time.Time
	NewID     func() string
}

// ExportService orchestrates ViewDefinition/$viewdefinition-export operations.
type ExportService struct {
	jobs      ViewExportJobStore
	executor  *Executor
	writer    ExportWriter
	watermark WatermarkAdvancer
	jobQueue  store.JobStore
	basePath  string
	now       func() time.Time
	newID     func() string
}

// NewExportService validates configuration and returns an ExportService.
func NewExportService(cfg ExportServiceConfig) (*ExportService, error) {
	if cfg.Jobs == nil {
		return nil, fmt.Errorf("view: export job store is required")
	}
	if cfg.Executor == nil {
		return nil, fmt.Errorf("view: executor is required")
	}
	if cfg.Writer == nil {
		return nil, fmt.Errorf("view: export writer is required")
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
	return &ExportService{
		jobs:      cfg.Jobs,
		executor:  cfg.Executor,
		writer:    cfg.Writer,
		watermark: cfg.Watermark,
		jobQueue:  cfg.JobQueue,
		basePath:  strings.TrimSuffix(cfg.BasePath, "/"),
		now:       cfg.Now,
		newID:     cfg.NewID,
	}, nil
}

// ViewExportJobPayload is the background job payload for export execution.
type ViewExportJobPayload struct {
	JobID string `json:"jobId"`
}

// Kickoff creates an async export job.
func (s *ExportService) Kickoff(ctx context.Context, req ViewExportRequest) (*ViewExportJob, error) {
	if s == nil {
		return nil, fmt.Errorf("view: export service is nil")
	}
	if len(req.Views) == 0 {
		return nil, fmt.Errorf("view: at least one view is required")
	}
	if req.Format == "" {
		req.Format = FormatNDJSON
	}
	id := s.newID()
	now := s.now()
	job := ViewExportJob{
		ID:        id,
		Status:    ExportInProgress,
		Request:   req,
		CreatedAt: now,
		Progress:  "0%",
	}
	if err := s.jobs.Create(ctx, job); err != nil {
		return nil, err
	}
	if s.jobQueue != nil {
		_, err := jobs.Enqueue(ctx, s.jobQueue, jobs.TypeViewExport, ViewExportJobPayload{JobID: id}, jobs.EnqueueOptions{Now: s.now})
		if err != nil {
			return nil, err
		}
	} else if err := s.RunJob(ctx, id); err != nil {
		return nil, err
	}
	return s.jobs.Get(ctx, id)
}

// GetJob returns the current export job state.
func (s *ExportService) GetJob(ctx context.Context, jobID string) (*ViewExportJob, error) {
	return s.jobs.Get(ctx, jobID)
}

// Cancel requests cancellation of an in-progress export job.
func (s *ExportService) Cancel(ctx context.Context, jobID string) error {
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status != ExportInProgress {
		return fmt.Errorf("view: job %s is not in progress", jobID)
	}
	job.Cancelled = true
	job.Status = ExportCancelled
	job.CompletedAt = s.now()
	return s.jobs.Update(ctx, *job)
}

// StatusURL returns the polling URL for a job.
func (s *ExportService) StatusURL(jobID string) string {
	return fmt.Sprintf("%s/ViewDefinition/$viewdefinition-export/status/%s", s.basePath, jobID)
}

// JobHandler returns a jobs.Handler for async execution.
func (s *ExportService) JobHandler() jobs.Handler {
	return jobs.HandlerFunc(func(ctx context.Context, job store.JobRecord) error {
		var payload ViewExportJobPayload
		if err := jobs.UnmarshalPayload(job.Payload, &payload); err != nil {
			return err
		}
		return s.RunJob(ctx, payload.JobID)
	})
}

// RunJob executes one export job synchronously.
func (s *ExportService) RunJob(ctx context.Context, jobID string) error {
	job, err := s.jobs.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Cancelled {
		return nil
	}

	since := job.Request.Since
	if since.IsZero() && s.watermark != nil && len(job.Request.Views) == 1 {
		target := job.Request.Views[0]
		wmSince, wmErr := s.watermark.Since(ctx, target.ViewName, target.Version)
		if wmErr != nil {
			job.Status = ExportError
			job.LastError = wmErr.Error()
			job.CompletedAt = s.now()
			return s.jobs.Update(ctx, *job)
		}
		since = wmSince
	}

	var files []ExportFile
	for i, target := range job.Request.Views {
		if job.Cancelled {
			return nil
		}
		result, execErr := s.executor.Execute(ctx, ExecuteRequest{
			ViewName: target.ViewName,
			Version:  target.Version,
			Actor:    job.Request.Actor,
			Since:    since,
		})
		if execErr != nil {
			job.Status = ExportError
			job.LastError = execErr.Error()
			job.CompletedAt = s.now()
			return s.jobs.Update(ctx, *job)
		}
		outputName := target.OutputName
		if outputName == "" {
			outputName = target.ViewName
		}
		filename := exportFilename(outputName, target.Version, job.Request.Format)
		if err := s.writer.WriteExport(ctx, filename, result, job.Request.Format); err != nil {
			job.Status = ExportError
			job.LastError = err.Error()
			job.CompletedAt = s.now()
			return s.jobs.Update(ctx, *job)
		}
		files = append(files, ExportFile{
			ViewName:   target.ViewName,
			Version:    target.Version,
			OutputName: outputName,
			Filename:   filename,
			RowCount:   len(result.Rows),
			Format:     string(job.Request.Format),
		})
		if s.watermark != nil {
			if err := s.watermark.Advance(ctx, target.ViewName, target.Version, s.now()); err != nil {
				job.Status = ExportError
				job.LastError = err.Error()
				job.CompletedAt = s.now()
				return s.jobs.Update(ctx, *job)
			}
		}
		job.Progress = fmt.Sprintf("%d%%", (i+1)*100/len(job.Request.Views))
	}

	job.Status = ExportComplete
	job.Files = files
	job.Progress = "100%"
	job.CompletedAt = s.now()
	return s.jobs.Update(ctx, *job)
}

func exportFilename(outputName, version string, format OutputFormat) string {
	if version == "" {
		version = "1.0.0"
	}
	ext := "ndjson"
	switch format {
	case FormatCSV:
		ext = "csv"
	case FormatParquet:
		ext = "parquet"
	case FormatJSON:
		ext = "json"
	}
	return fmt.Sprintf("%s-%s.%s", outputName, version, ext)
}
