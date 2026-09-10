package http

import (
	"bytes"
	"context"
	"net/http"
	"strings"
)

// ConformanceRefresher rebuilds live conformance state after installs.
type ConformanceRefresher interface {
	Refresh(ctx context.Context) error
}

// AdminConfig configures platform admin HTTP routes.
type AdminConfig struct {
	PackageInstallService PackageInstallService
	ModuleInstallService  ModuleInstallService
	JobStatusService      JobStatusService
	ConformanceRefresher  ConformanceRefresher
}

// NewAdminHandler serves /admin/* platform management routes.
func NewAdminHandler(cfg AdminConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/admin")
		path = strings.TrimPrefix(path, "/")
		switch {
		case path == "packages/install":
			handleAdminPackageInstall(w, r, cfg)
		case path == "modules/install":
			handleAdminModuleInstall(w, r, cfg)
		case path == "conformance/refresh":
			handleAdminConformanceRefresh(w, r, cfg)
		case strings.HasPrefix(path, "jobs/"):
			handleAdminJobStatus(w, r, cfg, strings.TrimPrefix(path, "jobs/"))
		default:
			writeError(w, unsupportedEndpoint(r.URL.Path))
		}
	})
}

func handleAdminPackageInstall(w http.ResponseWriter, r *http.Request, cfg AdminConfig) {
	if cfg.PackageInstallService == nil {
		writeError(w, notConfigured("package install service"))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
		return
	}
	body, err := readBodyAllowEmpty(r)
	if err != nil {
		writeError(w, err)
		return
	}
	contentType := r.Header.Get("Content-Type")
	if isPackageArchiveContentType(contentType) && len(body) > 0 {
		packageID := strings.TrimSpace(r.URL.Query().Get("packageId"))
		version := strings.TrimSpace(r.URL.Query().Get("version"))
		job, err := cfg.PackageInstallService.EnqueueArchiveInstall(r.Context(), packageID, version, bytes.NewReader(body))
		if err != nil {
			writeError(w, err)
			return
		}
		writeEnvelope(w, http.StatusAccepted, installJobParameters(job.ID, packageID, version), nil)
		return
	}
	packageID := strings.TrimSpace(r.URL.Query().Get("packageId"))
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	if packageID == "" || version == "" {
		paramID, paramVersion := parseInstallParameters(body)
		if packageID == "" {
			packageID = paramID
		}
		if version == "" {
			version = paramVersion
		}
	}
	if packageID == "" || version == "" {
		writeError(w, invalidRequest("packageId and version are required for registry install", nil))
		return
	}
	job, err := cfg.PackageInstallService.EnqueueRegistryInstall(r.Context(), packageID, version)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusAccepted, installJobParameters(job.ID, packageID, version), nil)
}

func handleAdminModuleInstall(w http.ResponseWriter, r *http.Request, cfg AdminConfig) {
	if cfg.ModuleInstallService == nil {
		writeError(w, notConfigured("module install service"))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
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
		writeError(w, invalidRequest("path is required for module install", nil))
		return
	}
	job, err := cfg.ModuleInstallService.EnqueueInstall(r.Context(), path, upgradeOnly)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusAccepted, moduleInstallJobParameters(job.ID, path), nil)
}

func handleAdminJobStatus(w http.ResponseWriter, r *http.Request, cfg AdminConfig, jobID string) {
	if cfg.JobStatusService == nil {
		writeError(w, notConfigured("job status service"))
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r.Method, http.MethodGet)
		return
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		writeError(w, invalidRequest("job id is required", nil))
		return
	}
	job, err := cfg.JobStatusService.GetJob(r.Context(), jobID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, jobStatusParameters(job), nil)
}

func handleAdminConformanceRefresh(w http.ResponseWriter, r *http.Request, cfg AdminConfig) {
	if cfg.ConformanceRefresher == nil {
		writeError(w, notConfigured("conformance refresher"))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
		return
	}
	if err := cfg.ConformanceRefresher.Refresh(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, parametersEnvelope([]map[string]any{
		{"name": "result", "valueBoolean": true},
	}), nil)
}
