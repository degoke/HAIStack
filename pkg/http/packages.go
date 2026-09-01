package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/jobs"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// PackageInstallService enqueues FHIR NPM package install jobs.
// Install jobs refresh conformance state automatically on completion.
type PackageInstallService interface {
	EnqueueRegistryInstall(ctx context.Context, packageID, version string) (store.JobRecord, error)
	EnqueueArchiveInstall(ctx context.Context, packageID, version string, r io.Reader) (store.JobRecord, error)
}

// CorePackageInstallService implements package install using registry and jobs.
type CorePackageInstallService struct {
	JobStore store.JobStore
}

func (s CorePackageInstallService) EnqueueRegistryInstall(ctx context.Context, packageID, version string) (store.JobRecord, error) {
	if s.JobStore == nil {
		return store.JobRecord{}, notConfigured("job store")
	}
	return jobs.Enqueue(ctx, s.JobStore, jobs.TypeRegistryPackageInstall, jobs.PackageInstallPayload{
		Source:    "registry",
		PackageID: packageID,
		Version:   version,
	}, jobs.EnqueueOptions{})
}

func (s CorePackageInstallService) EnqueueArchiveInstall(ctx context.Context, packageID, version string, r io.Reader) (store.JobRecord, error) {
	if s.JobStore == nil {
		return store.JobRecord{}, notConfigured("job store")
	}
	path, err := saveInstallArchive(r)
	if err != nil {
		return store.JobRecord{}, err
	}
	return jobs.Enqueue(ctx, s.JobStore, jobs.TypeRegistryPackageInstall, jobs.PackageInstallPayload{
		Source:    "upload",
		PackageID: packageID,
		Version:   version,
		Path:      path,
	}, jobs.EnqueueOptions{})
}

func saveInstallArchive(r io.Reader) (string, error) {
	f, err := os.CreateTemp("", "haistack-package-*.tgz")
	if err != nil {
		return "", fmt.Errorf("create temp package file: %w", err)
	}
	path := f.Name()
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write temp package file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close temp package file: %w", err)
	}
	return path, nil
}

func notConfigured(component string) error {
	return &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: component + " is not configured"}
}

func parseInstallParameters(body []byte) (packageID, version string) {
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
		case "id", "package", "packageId":
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

func installJobParameters(jobID, packageID, version string) *types.ResourceEnvelope {
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

func isPackageArchiveContentType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch ct {
	case "application/gzip", "application/x-gzip", "application/octet-stream", "application/tar+gzip":
		return true
	default:
		return false
	}
}
