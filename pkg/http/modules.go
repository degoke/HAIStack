package http

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ModuleInstallService enqueues local module install jobs.
type ModuleInstallService interface {
	EnqueueInstall(ctx context.Context, path string, upgradeOnly bool) (store.JobRecord, error)
}

// CoreModuleInstallService implements module install using jobs.
type CoreModuleInstallService struct {
	JobStore store.JobStore
}

func (s CoreModuleInstallService) EnqueueInstall(ctx context.Context, path string, upgradeOnly bool) (store.JobRecord, error) {
	if s.JobStore == nil {
		return store.JobRecord{}, notConfigured("job store")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return store.JobRecord{}, invalidRequest("path is required for module install", nil)
	}
	return jobs.Enqueue(ctx, s.JobStore, jobs.TypeModuleInstall, jobs.ModuleInstallPayload{
		Path:        path,
		UpgradeOnly: upgradeOnly,
	}, jobs.EnqueueOptions{})
}

func parseModuleInstallParameters(body []byte) (path string, upgradeOnly bool) {
	if len(body) == 0 {
		return "", false
	}
	var params struct {
		Parameter []struct {
			Name         string `json:"name"`
			ValueString  string `json:"valueString,omitempty"`
			ValueBoolean bool   `json:"valueBoolean,omitempty"`
		} `json:"parameter"`
	}
	if err := json.Unmarshal(body, &params); err != nil {
		return "", false
	}
	for _, p := range params.Parameter {
		switch p.Name {
		case "path":
			path = strings.TrimSpace(p.ValueString)
		case "upgradeOnly":
			upgradeOnly = p.ValueBoolean
		}
	}
	return path, upgradeOnly
}

func parseTerminologyInstallScope(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var params struct {
		Parameter []struct {
			Name        string `json:"name"`
			ValueString string `json:"valueString,omitempty"`
		} `json:"parameter"`
	}
	if err := json.Unmarshal(body, &params); err != nil {
		return ""
	}
	for _, p := range params.Parameter {
		if p.Name == "scopeId" {
			return strings.TrimSpace(p.ValueString)
		}
	}
	return ""
}

func terminologyInstallJobParameters(jobID, scopeID string) *types.ResourceEnvelope {
	payload := map[string]any{
		"resourceType": "Parameters",
		"parameter": []map[string]any{
			{"name": "jobId", "valueString": jobID},
			{"name": "scopeId", "valueString": scopeID},
			{"name": "status", "valueString": "accepted"},
		},
	}
	raw, _ := json.Marshal(payload)
	return &types.ResourceEnvelope{ResourceType: "Parameters", JSON: raw}
}

func moduleInstallJobParameters(jobID, path string) *types.ResourceEnvelope {
	payload := map[string]any{
		"resourceType": "Parameters",
		"parameter": []map[string]any{
			{"name": "jobId", "valueString": jobID},
			{"name": "path", "valueString": path},
			{"name": "status", "valueString": "accepted"},
		},
	}
	raw, _ := json.Marshal(payload)
	return &types.ResourceEnvelope{ResourceType: "Parameters", JSON: raw}
}
