package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/packages"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// PackageInstallService installs FHIR NPM packages and refreshes conformance state.
type PackageInstallService interface {
	InstallFromRegistry(ctx context.Context, packageID, version string) (*packages.InstallResult, error)
	InstallFromDirectory(ctx context.Context, path string) (*packages.InstallResult, error)
	InstallFromUpload(ctx context.Context, packageID, version string, r io.Reader) (*packages.InstallResult, error)
	EnqueueInstall(ctx context.Context, payload jobs.PackageInstallPayload) (store.JobRecord, error)
	Refresh(ctx context.Context) (*registry.Snapshot, error)
}

// CorePackageInstallService implements package install using registry and jobs.
type CorePackageInstallService struct {
	Installer *packages.Installer
	Runtime   ConformanceRuntime
	JobStore  store.JobStore
}

func (s CorePackageInstallService) InstallFromRegistry(ctx context.Context, packageID, version string) (*packages.InstallResult, error) {
	if s.Installer == nil {
		return nil, notConfigured("package installer")
	}
	return s.Installer.InstallFromRegistry(ctx, packageID, version)
}

func (s CorePackageInstallService) InstallFromDirectory(ctx context.Context, path string) (*packages.InstallResult, error) {
	if s.Installer == nil {
		return nil, notConfigured("package installer")
	}
	return s.Installer.InstallFromDirectory(ctx, path)
}

func (s CorePackageInstallService) InstallFromUpload(ctx context.Context, packageID, version string, r io.Reader) (*packages.InstallResult, error) {
	if s.Installer == nil {
		return nil, notConfigured("package installer")
	}
	return s.Installer.InstallFromArchive(ctx, packageID, version, r)
}

func (s CorePackageInstallService) EnqueueInstall(ctx context.Context, payload jobs.PackageInstallPayload) (store.JobRecord, error) {
	if s.JobStore == nil {
		return store.JobRecord{}, notConfigured("job store")
	}
	return jobs.Enqueue(ctx, s.JobStore, jobs.TypeRegistryPackageInstall, payload, jobs.EnqueueOptions{})
}

func (s CorePackageInstallService) Refresh(ctx context.Context) (*registry.Snapshot, error) {
	if s.Runtime == nil {
		return nil, notConfigured("conformance runtime")
	}
	return s.Runtime.Refresh(ctx)
}

func notConfigured(component string) error {
	return &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: component + " is not configured"}
}

// NPMOperationService implements FHIR $npm package install.
type NPMOperationService struct {
	Packages PackageInstallService
}

// Execute handles POST /fhir/$npm.
func (s NPMOperationService) Execute(ctx context.Context, req OperationRequest) (*types.ResourceEnvelope, error) {
	if s.Packages == nil {
		return nil, notConfigured("package install service")
	}
	if req.Operation != "$npm" {
		return nil, unsupportedEndpoint(req.Operation)
	}
	packageID := strings.TrimSpace(req.Query.Get("id"))
	version := strings.TrimSpace(req.Query.Get("version"))
	if packageID == "" || version == "" {
		packageID, version = parseNPMPackageParameters(req.Body)
	}
	if packageID == "" || version == "" {
		return nil, invalidRequest("package id and version are required", nil)
	}
	async := truthy(req.Query.Get("async"))
	if async {
		job, err := s.Packages.EnqueueInstall(ctx, jobs.PackageInstallPayload{
			Source:    "registry",
			PackageID: packageID,
			Version:   version,
		})
		if err != nil {
			return nil, err
		}
		return npmJobParameters(job.ID, packageID, version), nil
	}
	result, err := s.Packages.InstallFromRegistry(ctx, packageID, version)
	if err != nil {
		return nil, err
	}
	return npmResultParameters(result), nil
}

func parseNPMPackageParameters(body []byte) (packageID, version string) {
	if len(body) == 0 {
		return "", ""
	}
	var params struct {
		Parameter []struct {
			Name           string `json:"name"`
			ValueString    string `json:"valueString,omitempty"`
			ValueCanonical string `json:"valueCanonical,omitempty"`
			ValueUri       string `json:"valueUri,omitempty"`
		} `json:"parameter"`
	}
	if err := json.Unmarshal(body, &params); err != nil {
		return "", ""
	}
	for _, p := range params.Parameter {
		switch p.Name {
		case "id", "package":
			packageID = firstNonEmpty(p.ValueString, p.ValueCanonical, p.ValueUri)
		case "version":
			version = firstNonEmpty(p.ValueString, p.ValueCanonical, p.ValueUri)
		}
	}
	return packageID, version
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func npmResultParameters(result *packages.InstallResult) *types.ResourceEnvelope {
	payload := map[string]any{
		"resourceType": "Parameters",
		"parameter": []map[string]any{
			{"name": "packageId", "valueString": result.PackageID},
			{"name": "version", "valueString": result.Version},
			{"name": "installedDefinitions", "valueInteger": result.Installed},
		},
	}
	if len(result.Enabled) > 0 {
		payload["parameter"] = append(payload["parameter"].([]map[string]any), map[string]any{
			"name": "enabledResources",
			"valueString": strings.Join(result.Enabled, ","),
		})
	}
	raw, _ := json.Marshal(payload)
	return &types.ResourceEnvelope{ResourceType: "Parameters", JSON: raw}
}

func npmJobParameters(jobID, packageID, version string) *types.ResourceEnvelope {
	payload := map[string]any{
		"resourceType": "Parameters",
		"parameter": []map[string]any{
			{"name": "jobId", "valueString": jobID},
			{"name": "packageId", "valueString": packageID},
			{"name": "version", "valueString": version},
			{"name": "status", "valueString": "accepted"},
		},
	}
	raw, _ := json.Marshal(payload)
	return &types.ResourceEnvelope{ResourceType: "Parameters", JSON: raw}
}

// AdminHandler serves package install and conformance refresh endpoints.
type AdminHandler struct {
	Packages PackageInstallService
}

func NewAdminHandler(packages PackageInstallService) http.Handler {
	return &AdminHandler{Packages: packages}
}

func (h *AdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Packages == nil {
		writeError(w, notConfigured("admin package service"))
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/admin/packages/install":
		h.handleInstall(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/admin/packages/upload":
		h.handleUpload(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/admin/conformance/refresh":
		h.handleRefresh(w, r)
	default:
		writeError(w, unsupportedEndpoint(r.URL.Path))
	}
}

type adminInstallRequest struct {
	Source    string `json:"source"`
	PackageID string `json:"packageId"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	Async     bool   `json:"async"`
}

func (h *AdminHandler) handleInstall(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var req adminInstallRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, invalidRequest("parse install request", err))
		return
	}
	if req.Async {
		job, err := h.Packages.EnqueueInstall(r.Context(), jobs.PackageInstallPayload{
			Source:    req.Source,
			PackageID: req.PackageID,
			Version:   req.Version,
			Path:      req.Path,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"jobId": job.ID, "status": "accepted"})
		return
	}
	result, err := h.installSync(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *AdminHandler) installSync(ctx context.Context, req adminInstallRequest) (*packages.InstallResult, error) {
	switch req.Source {
	case "registry":
		return h.Packages.InstallFromRegistry(ctx, req.PackageID, req.Version)
	case "path":
		return h.Packages.InstallFromDirectory(ctx, req.Path)
	default:
		return nil, invalidRequest("source must be registry or path", nil)
	}
}

func (h *AdminHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeError(w, invalidRequest("parse multipart form", err))
		return
	}
	file, _, err := r.FormFile("package")
	if err != nil {
		writeError(w, invalidRequest("package file is required", err))
		return
	}
	defer func() { _ = file.Close() }()
	packageID := strings.TrimSpace(r.FormValue("packageId"))
	version := strings.TrimSpace(r.FormValue("version"))
	result, err := h.Packages.InstallFromUpload(r.Context(), packageID, version, file)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *AdminHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.Packages.Refresh(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "refreshed",
		"enabledTypes": snapshot.EnabledResourceTypes(),
		"compiledAt":   snapshot.CapabilitySnapshot().CompiledAt,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
