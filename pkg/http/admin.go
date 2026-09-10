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
	ConformanceRefresher  ConformanceRefresher
}

// NewAdminHandler serves /admin/* platform management routes.
func NewAdminHandler(cfg AdminConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/admin")
		path = strings.TrimPrefix(path, "/")
		switch path {
		case "packages/install":
			handleAdminPackageInstall(w, r, cfg)
		case "conformance/refresh":
			handleAdminConformanceRefresh(w, r, cfg)
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
