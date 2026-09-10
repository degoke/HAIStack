package http

import (
	"context"
	"net/http"
	"strings"
)

// ConformanceRefresher rebuilds live conformance state after installs.
type ConformanceRefresher interface {
	Refresh(ctx context.Context) error
}

func (h *handler) handlePlatformOperation(w http.ResponseWriter, r *http.Request, route parsedRoute) bool {
	switch route.resourceType {
	case "Basic":
		switch route.operation {
		case "$install":
			if route.id != "" {
				return false
			}
			h.handleBasicModuleInstall(w, r, route)
			return true
		case "$status":
			if route.id == "" {
				return false
			}
			h.handleBasicJobStatus(w, r, route)
			return true
		case "$terminology-install":
			if route.id != "" {
				return false
			}
			h.handleBasicTerminologyInstall(w, r, route)
			return true
		}
	case "CapabilityStatement":
		if route.operation == "$refresh" && route.id == "" {
			h.handleCapabilityStatementRefresh(w, r, route)
			return true
		}
	}
	return false
}

func (h *handler) handleBasicModuleInstall(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.ModuleInstallService == nil {
		writeError(w, notConfigured("module install service"))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
		return
	}
	if err := h.authorizeOperation(r.Context(), route.resourceType, route.operation, route.id); err != nil {
		writeError(w, err)
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	upgradeOnly := strings.EqualFold(r.URL.Query().Get("upgradeOnly"), "true")
	body, err := readBodyAllowEmpty(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if path == "" {
		paramPath, paramUpgrade := parseModuleInstallParameters(body)
		path = paramPath
		if paramUpgrade {
			upgradeOnly = true
		}
	}
	if path == "" {
		writeError(w, invalidRequest("path is required for $install", nil))
		return
	}
	path, err = validateModulePath(path, h.cfg.ModulePaths)
	if err != nil {
		writeError(w, err)
		return
	}
	job, err := h.cfg.ModuleInstallService.EnqueueInstall(r.Context(), path, upgradeOnly)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusAccepted, moduleInstallJobParameters(job.ID, path), nil)
}

func (h *handler) handleBasicJobStatus(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.JobStatusService == nil {
		writeError(w, notConfigured("job status service"))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost)
		return
	}
	if err := h.authorizeOperation(r.Context(), route.resourceType, route.operation, route.id); err != nil {
		writeError(w, err)
		return
	}
	jobID := strings.TrimSpace(route.id)
	if jobID == "" {
		jobID = strings.TrimSpace(r.URL.Query().Get("jobId"))
	}
	if jobID == "" {
		writeError(w, invalidRequest("job id is required for $status", nil))
		return
	}
	job, err := h.cfg.JobStatusService.GetJob(r.Context(), jobID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, jobStatusParameters(job), nil)
}

func (h *handler) handleCapabilityStatementRefresh(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.ConformanceRefresher == nil {
		writeError(w, notConfigured("conformance refresher"))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
		return
	}
	if err := h.authorizeOperation(r.Context(), route.resourceType, route.operation, route.id); err != nil {
		writeError(w, err)
		return
	}
	if err := h.cfg.ConformanceRefresher.Refresh(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, parametersEnvelope([]map[string]any{
		{"name": "result", "valueBoolean": true},
	}), nil)
}

func (h *handler) handleBasicTerminologyInstall(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.TerminologyInstallService == nil {
		writeError(w, notConfigured("terminology install service"))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
		return
	}
	if err := h.authorizeOperation(r.Context(), route.resourceType, route.operation, route.id); err != nil {
		writeError(w, err)
		return
	}
	scopeID := strings.TrimSpace(r.URL.Query().Get("scopeId"))
	body, err := readBodyAllowEmpty(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if scopeID == "" {
		scopeID = parseTerminologyInstallScope(body)
	}
	job, err := h.cfg.TerminologyInstallService.EnqueueRebuild(r.Context(), scopeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusAccepted, terminologyInstallJobParameters(job.ID, scopeID), nil)
}
