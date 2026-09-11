package http

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// TerminologyInstallService enqueues async terminology projection rebuild jobs.
type TerminologyInstallService interface {
	EnqueueRebuild(ctx context.Context, scopeID string, preExpandValueSets bool) (store.JobRecord, error)
}

// CoreTerminologyInstallService implements terminology rebuild using jobs.
type CoreTerminologyInstallService struct {
	JobStore     store.JobStore
	DefaultScope string
}

func (s CoreTerminologyInstallService) EnqueueRebuild(ctx context.Context, scopeID string, preExpandValueSets bool) (store.JobRecord, error) {
	if s.JobStore == nil {
		return store.JobRecord{}, notConfigured("job store")
	}
	if scopeID == "" {
		scopeID = s.DefaultScope
	}
	if scopeID == "" {
		return store.JobRecord{}, invalidRequest("scopeId is required for terminology install", nil)
	}
	return enqueueJob(ctx, s.JobStore, jobs.TypeTerminologyInstall, jobs.TerminologyInstallPayload{
		ScopeID:            scopeID,
		PreExpandValueSets: preExpandValueSets,
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

// TerminologyEnableService enables or disables tenant opt-in to global catalog entries.
type TerminologyEnableService interface {
	Enable(ctx context.Context, record store.TerminologyInstallRecord) (terminology.EnableResult, error)
}

// CoreTerminologyEnableService implements terminology catalog opt-in.
type CoreTerminologyEnableService struct {
	InstallFactory  store.TerminologyInstallStoreFactory
	DefaultTenantID string
	Global          store.TerminologyStore
	Definitions     store.DefinitionStore
}

func (s CoreTerminologyEnableService) Enable(ctx context.Context, record store.TerminologyInstallRecord) (terminology.EnableResult, error) {
	installs := store.TerminologyInstallsFromContext(ctx)
	if installs == nil && s.InstallFactory != nil && s.DefaultTenantID != "" {
		resolved, err := s.InstallFactory.ForTenant(ctx, s.DefaultTenantID)
		if err != nil {
			return terminology.EnableResult{}, err
		}
		installs = resolved
	}
	if installs == nil {
		return terminology.EnableResult{}, notConfigured("terminology install store")
	}
	opts := terminology.CatalogEnableOptions{
		Global:      s.Global,
		Installs:    installs,
		Definitions: s.Definitions,
	}
	if record.PackName != "" && record.CanonicalURL == "" {
		result, err := terminology.EnableCatalogPack(ctx, opts, record.PackName, record.PackVersion, record.Enabled)
		if err != nil {
			return terminology.EnableResult{}, mapTerminologyEnableError(err)
		}
		return result, nil
	}
	if record.CanonicalURL == "" {
		return terminology.EnableResult{}, invalidRequest("canonicalUrl or packName is required for terminology enable", nil)
	}
	result, err := terminology.EnableCatalogEntry(ctx, opts, record)
	if err != nil {
		return terminology.EnableResult{}, mapTerminologyEnableError(err)
	}
	return result, nil
}

func mapTerminologyEnableError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "not found in global scope") || strings.Contains(msg, "no terminology resources found") {
		return &core.ServiceError{Kind: core.ErrorKindNotFound, Message: msg}
	}
	if strings.Contains(msg, "global terminology store is required") {
		return notConfigured("global terminology store")
	}
	return invalidRequest(msg, err)
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
