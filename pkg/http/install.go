package http

import (
	"bytes"
	"net/http"
	"strings"
)

func (h *handler) handleImplementationGuideInstall(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.PackageInstallService == nil {
		writeError(w, notConfigured("package install service"))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
		return
	}
	if err := h.authorizeWrite(r.Context(), "operation", route.resourceType, route.id); err != nil {
		writeError(w, err)
		return
	}

	body, err := readBodyAllowEmpty(r)
	if err != nil {
		writeError(w, err)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if isPackageArchiveContentType(contentType) && len(body) > 0 {
		h.handleImplementationGuideInstallArchive(w, r, body)
		return
	}

	packageID := strings.TrimSpace(r.URL.Query().Get("packageId"))
	if packageID == "" {
		packageID = strings.TrimSpace(r.URL.Query().Get("id"))
	}
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

	job, err := h.cfg.PackageInstallService.EnqueueRegistryInstall(r.Context(), packageID, version)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusAccepted, installJobParameters(job.ID, packageID, version), nil)
}

func (h *handler) handleImplementationGuideInstallArchive(w http.ResponseWriter, r *http.Request, body []byte) {
	packageID := strings.TrimSpace(r.URL.Query().Get("packageId"))
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	reader := bytes.NewReader(body)

	job, err := h.cfg.PackageInstallService.EnqueueArchiveInstall(r.Context(), packageID, version, reader)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusAccepted, installJobParameters(job.ID, packageID, version), nil)
}
