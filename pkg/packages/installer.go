package packages

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/registry"
)

const defaultRegistryBase = "https://packages.fhir.org"

// Installer ingests FHIR NPM packages into the registry catalog.
type Installer struct {
	Registry     *registry.Manager
	Refresh      func(ctx context.Context) error
	RegistryBase string
	HTTPClient   *http.Client
	TempDir      string
	EnableTypes  bool
}

// InstallResult summarizes a package install.
type InstallResult struct {
	PackageID   string   `json:"packageId"`
	Version     string   `json:"version"`
	Installed   int      `json:"installedDefinitions"`
	Enabled     []string `json:"enabledResources,omitempty"`
	ExtractedTo string   `json:"extractedTo,omitempty"`
}

// InstallFromRegistry downloads and installs a package from packages.fhir.org.
func (i *Installer) InstallFromRegistry(ctx context.Context, packageID, version string) (*InstallResult, error) {
	if i == nil || i.Registry == nil {
		return nil, fmt.Errorf("package installer is not configured")
	}
	packageID = strings.TrimSpace(packageID)
	version = strings.TrimSpace(version)
	if packageID == "" || version == "" {
		return nil, fmt.Errorf("package id and version are required")
	}
	base := i.registryBase()
	url := fmt.Sprintf("%s/%s/%s", base, packageID, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := i.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download package: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download package: HTTP %d from %s", resp.StatusCode, url)
	}
	return i.InstallFromArchive(ctx, packageID, version, resp.Body)
}

// InstallFromDirectory installs all JSON definitions from a local directory tree.
func (i *Installer) InstallFromDirectory(ctx context.Context, dir string) (*InstallResult, error) {
	if i == nil || i.Registry == nil {
		return nil, fmt.Errorf("package installer is not configured")
	}
	definitions, err := loadPackageDefinitions(dir)
	if err != nil {
		return nil, err
	}
	packageID := filepath.Base(dir)
	return i.installDefinitions(ctx, packageID, "local", dir, definitions)
}

// InstallFromArchive extracts a FHIR NPM tarball and installs package resources.
func (i *Installer) InstallFromArchive(ctx context.Context, packageID, version string, r io.Reader) (*InstallResult, error) {
	if i == nil || i.Registry == nil {
		return nil, fmt.Errorf("package installer is not configured")
	}
	dir, err := i.extractArchive(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	packageDir := filepath.Join(dir, "package")
	if stat, statErr := os.Stat(packageDir); statErr != nil || !stat.IsDir() {
		packageDir = dir
	}
	definitions, err := loadPackageDefinitions(packageDir)
	if err != nil {
		return nil, err
	}
	if packageID == "" {
		packageID = packageNameFromDefinitions(definitions)
	}
	if version == "" {
		version = "unknown"
	}
	return i.installDefinitions(ctx, packageID, version, packageDir, definitions)
}

func (i *Installer) installDefinitions(ctx context.Context, packageID, version, sourceDir string, definitions [][]byte) (*InstallResult, error) {
	if err := i.Registry.SeedBundled(ctx); err != nil {
		return nil, err
	}
	provenance := registry.InstallProvenance{
		PackageName:    packageID,
		PackageVersion: version,
		ModuleName:     packageID,
		SourceModule:   packageID,
	}
	result := &InstallResult{
		PackageID:   packageID,
		Version:     version,
		ExtractedTo: sourceDir,
	}
	enabled := make(map[string]struct{})
	for _, raw := range definitions {
		parsed, _, err := registry.ParseDefinition(raw)
		if err != nil {
			return nil, fmt.Errorf("parse definition: %w", err)
		}
		if err := i.Registry.InstallDefinition(ctx, raw, provenance); err != nil {
			return nil, fmt.Errorf("install definition %s: %w", parsed.CanonicalURL, err)
		}
		result.Installed++
		if i.EnableTypes && parsed.FHIRResourceType == "StructureDefinition" {
			if resourceType := structureDefinitionResourceType(raw); resourceType != "" {
				enabled[resourceType] = struct{}{}
			}
		}
	}
	for resourceType := range enabled {
		if err := i.Registry.EnableResource(ctx, resourceType); err != nil {
			if errors.Is(err, registry.ErrMissingDefinition) {
				continue
			}
			return nil, fmt.Errorf("enable resource type %s: %w", resourceType, err)
		}
		result.Enabled = append(result.Enabled, resourceType)
	}
	if i.Refresh != nil {
		if err := i.Refresh(ctx); err != nil {
			return result, fmt.Errorf("refresh conformance runtime: %w", err)
		}
	}
	return result, nil
}

func (i *Installer) extractArchive(r io.Reader) (string, error) {
	root, err := os.MkdirTemp(i.TempDir, "haistack-package-*")
	if err != nil {
		return "", err
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		_ = os.RemoveAll(root)
		return "", fmt.Errorf("read gzip archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = os.RemoveAll(root)
			return "", fmt.Errorf("read tar archive: %w", err)
		}
		target := filepath.Join(root, filepath.Clean(hdr.Name))
		if !strings.HasPrefix(target, filepath.Clean(root)+string(os.PathSeparator)) && target != filepath.Clean(root) {
			_ = os.RemoveAll(root)
			return "", fmt.Errorf("archive entry escapes target directory")
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				_ = os.RemoveAll(root)
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				_ = os.RemoveAll(root)
				return "", err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				_ = os.RemoveAll(root)
				return "", err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				_ = os.RemoveAll(root)
				return "", err
			}
			_ = out.Close()
		}
	}
	return root, nil
}

func loadPackageDefinitions(dir string) ([][]byte, error) {
	var out [][]byte
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".json") {
			return nil
		}
		if strings.EqualFold(d.Name(), "package.json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !isFHIRDefinitionJSON(data) {
			return nil
		}
		out = append(out, data)
		return nil
	})
	return out, err
}

func isFHIRDefinitionJSON(data []byte) bool {
	var envelope struct {
		ResourceType string `json:"resourceType"`
		URL          string `json:"url"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return false
	}
	return strings.TrimSpace(envelope.ResourceType) != "" && strings.TrimSpace(envelope.URL) != ""
}

func structureDefinitionResourceType(raw []byte) string {
	var sd struct {
		Kind string `json:"kind"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &sd); err != nil {
		return ""
	}
	if strings.EqualFold(sd.Kind, "resource") {
		return strings.TrimSpace(sd.Type)
	}
	return ""
}

func packageNameFromDefinitions(definitions [][]byte) string {
	for _, raw := range definitions {
		var meta struct {
			ResourceType string `json:"resourceType"`
			Name         string `json:"name"`
			URL          string `json:"url"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		if meta.ResourceType == "ImplementationGuide" && meta.Name != "" {
			return meta.Name
		}
		if meta.URL != "" {
			return meta.URL
		}
	}
	return "unknown-package"
}

func (i *Installer) registryBase() string {
	if strings.TrimSpace(i.RegistryBase) == "" {
		return defaultRegistryBase
	}
	return strings.TrimRight(strings.TrimSpace(i.RegistryBase), "/")
}
