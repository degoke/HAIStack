package http

import (
	"context"
	"encoding/json"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// TerminologyInstallService enqueues async terminology projection rebuild jobs.
type TerminologyInstallService interface {
	EnqueueRebuild(ctx context.Context, scopeID string) (store.JobRecord, error)
}

// CoreTerminologyInstallService implements terminology rebuild using jobs.
type CoreTerminologyInstallService struct {
	JobStore     store.JobStore
	DefaultScope string
}

func (s CoreTerminologyInstallService) EnqueueRebuild(ctx context.Context, scopeID string) (store.JobRecord, error) {
	if s.JobStore == nil {
		return store.JobRecord{}, notConfigured("job store")
	}
	if scopeID == "" {
		scopeID = s.DefaultScope
	}
	if scopeID == "" {
		return store.JobRecord{}, invalidRequest("scopeId is required for terminology install", nil)
	}
	return jobs.Enqueue(ctx, s.JobStore, jobs.TypeTerminologyInstall, jobs.TerminologyInstallPayload{
		ScopeID: scopeID,
	}, jobs.EnqueueOptions{})
}

// JobStatusService reads durable background job state.
type JobStatusService interface {
	GetJob(ctx context.Context, id string) (*store.JobRecord, error)
}

// CoreJobStatusService implements JobStatusService using store.JobStore.
type CoreJobStatusService struct {
	JobStore store.JobStore
}

func (s CoreJobStatusService) GetJob(ctx context.Context, id string) (*store.JobRecord, error) {
	if s.JobStore == nil {
		return nil, notConfigured("job store")
	}
	return s.JobStore.Get(ctx, id)
}

func jobStatusParameters(job *store.JobRecord) *types.ResourceEnvelope {
	params := []map[string]any{
		{"name": "jobId", "valueString": job.ID},
		{"name": "type", "valueString": job.Type},
		{"name": "status", "valueString": string(job.Status)},
		{"name": "attempts", "valueInteger": job.Attempts},
	}
	if job.LastError != "" {
		params = append(params, map[string]any{"name": "lastError", "valueString": job.LastError})
	}
	if progress, err := jobs.ProgressFromPayload(job.Payload); err == nil && progress != nil {
		if progress.Phase != "" {
			params = append(params, map[string]any{"name": "phase", "valueString": progress.Phase})
		}
		if progress.Message != "" {
			params = append(params, map[string]any{"name": "message", "valueString": progress.Message})
		}
		if progress.Total > 0 {
			params = append(params, map[string]any{"name": "total", "valueInteger": progress.Total})
		}
		if progress.Current > 0 {
			params = append(params, map[string]any{"name": "current", "valueInteger": progress.Current})
		}
	}
	if result, err := jobs.ResultFromPayload(job.Payload); err == nil && result != nil {
		raw, _ := json.Marshal(result)
		params = append(params, map[string]any{"name": "result", "valueString": string(raw)})
	}
	raw, _ := json.Marshal(map[string]any{
		"resourceType": "Parameters",
		"parameter":    params,
	})
	return &types.ResourceEnvelope{ResourceType: "Parameters", JSON: raw}
}
