package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/terminology"
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
	return enqueueJob(ctx, s.JobStore, jobs.TypeModuleInstall, jobs.ModuleInstallPayload{
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

func parseTerminologyInstallParameters(body []byte) (scopeID string, preExpand bool) {
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
		case "scopeId":
			scopeID = strings.TrimSpace(p.ValueString)
		case "preExpandValueSets":
			preExpand = p.ValueBoolean
		}
	}
	return scopeID, preExpand
}

func terminologyEnableRecordFromRequest(r *http.Request) store.TerminologyInstallRecord {
	enabled := true
	if v := strings.TrimSpace(r.URL.Query().Get("enabled")); v != "" {
		enabled = strings.EqualFold(v, "true")
	}
	return store.TerminologyInstallRecord{
		CanonicalURL: strings.TrimSpace(r.URL.Query().Get("canonicalUrl")),
		Version:      strings.TrimSpace(r.URL.Query().Get("version")),
		ResourceType: strings.TrimSpace(r.URL.Query().Get("resourceType")),
		PackName:     strings.TrimSpace(r.URL.Query().Get("packName")),
		PackVersion:  strings.TrimSpace(r.URL.Query().Get("packVersion")),
		Enabled:      enabled,
	}
}

func parseTerminologyEnableParameters(body []byte, defaults store.TerminologyInstallRecord) store.TerminologyInstallRecord {
	if len(body) == 0 {
		return defaults
	}
	var params struct {
		Parameter []struct {
			Name         string `json:"name"`
			ValueString  string `json:"valueString,omitempty"`
			ValueBoolean bool   `json:"valueBoolean,omitempty"`
		} `json:"parameter"`
	}
	if err := json.Unmarshal(body, &params); err != nil {
		return defaults
	}
	record := defaults
	for _, p := range params.Parameter {
		switch p.Name {
		case "canonicalUrl":
			record.CanonicalURL = strings.TrimSpace(p.ValueString)
		case "version":
			record.Version = strings.TrimSpace(p.ValueString)
		case "resourceType":
			record.ResourceType = strings.TrimSpace(p.ValueString)
		case "packName":
			record.PackName = strings.TrimSpace(p.ValueString)
		case "packVersion":
			record.PackVersion = strings.TrimSpace(p.ValueString)
		case "enabled":
			record.Enabled = p.ValueBoolean
		}
	}
	return record
}

func terminologyEnableParameters(record store.TerminologyInstallRecord, result terminology.EnableResult) *types.ResourceEnvelope {
	params := []map[string]any{
		{"name": "enabled", "valueBoolean": record.Enabled},
		{"name": "count", "valueInteger": result.Count},
	}
	if record.CanonicalURL != "" {
		params = append(params,
			map[string]any{"name": "canonicalUrl", "valueString": record.CanonicalURL},
			map[string]any{"name": "version", "valueString": record.Version},
			map[string]any{"name": "resourceType", "valueString": record.ResourceType},
		)
	}
	if record.PackName != "" {
		params = append(params,
			map[string]any{"name": "packName", "valueString": record.PackName},
			map[string]any{"name": "packVersion", "valueString": record.PackVersion},
		)
	}
	for _, warning := range result.Warnings {
		params = append(params, map[string]any{"name": "warning", "valueString": warning})
	}
	raw, _ := json.Marshal(map[string]any{"resourceType": "Parameters", "parameter": params})
	return &types.ResourceEnvelope{ResourceType: "Parameters", JSON: raw}
}

func terminologyInstallJobParameters(jobID, scopeID string, preExpand bool) *types.ResourceEnvelope {
	payload := map[string]any{
		"resourceType": "Parameters",
		"parameter": []map[string]any{
			{"name": "jobId", "valueString": jobID},
			{"name": "scopeId", "valueString": scopeID},
			{"name": "preExpandValueSets", "valueBoolean": preExpand},
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
