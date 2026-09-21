package cql

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

type fhirLibrary struct {
	ResourceType string `json:"resourceType"`
	ID           string `json:"id"`
	URL          string `json:"url"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Content      []struct {
		ContentType string `json:"contentType"`
		Data        string `json:"data"`
		URL         string `json:"url"`
	} `json:"content"`
}

func parseLibraryEnvelope(env *types.ResourceEnvelope) (source, url, name, version string, err error) {
	if env == nil || len(env.JSON) == 0 {
		return "", "", "", "", errf("CQL Library envelope is empty")
	}
	return parseLibraryJSON(env.JSON)
}

func peekLibraryMeta(env *types.ResourceEnvelope) (url, name, version string) {
	if env == nil || len(env.JSON) == 0 {
		return "", "", ""
	}
	var lib fhirLibrary
	if err := json.Unmarshal(env.JSON, &lib); err != nil {
		return "", "", ""
	}
	return lib.URL, lib.Name, lib.Version
}

func parseLibraryJSON(data []byte) (source, url, name, version string, err error) {
	var lib fhirLibrary
	if err := json.Unmarshal(data, &lib); err != nil {
		return "", "", "", "", errf("decode CQL Library: %w", err)
	}
	if lib.ResourceType != "" && lib.ResourceType != "Library" {
		return "", "", "", "", errf("expected Library resource, got %s", lib.ResourceType)
	}
	src, err := libraryCQLSource(lib)
	if err != nil {
		return "", "", "", "", err
	}
	return src, lib.URL, lib.Name, lib.Version, nil
}

func libraryCQLSource(lib fhirLibrary) (string, error) {
	var elmOnly bool
	for _, c := range lib.Content {
		ct := strings.ToLower(strings.TrimSpace(c.ContentType))
		if isCQLContentType(ct) {
			if strings.TrimSpace(c.Data) == "" {
				continue
			}
			raw, err := decodeLibraryData(c.Data)
			if err != nil {
				return "", err
			}
			src := strings.TrimSpace(string(raw))
			if src == "" {
				continue
			}
			return src, nil
		}
		if strings.Contains(ct, "elm") {
			elmOnly = true
		}
	}
	if elmOnly {
		return "", errf("%w: Library %q is ELM-only; CQL source (text/cql) is required", ErrUnsupported, libraryLabel(lib))
	}
	return "", errf("%w: Library %q has no text/cql content", ErrLibraryNotFound, libraryLabel(lib))
}

func libraryLabel(lib fhirLibrary) string {
	if lib.URL != "" {
		return lib.URL
	}
	if lib.Name != "" {
		return lib.Name
	}
	return lib.ID
}

func isCQLContentType(ct string) bool {
	switch {
	case ct == "text/cql", ct == "application/cql", ct == "application/x-cql":
		return true
	case strings.HasPrefix(ct, "text/cql"):
		return true
	}
	return false
}

func decodeLibraryData(data string) ([]byte, error) {
	data = strings.TrimSpace(data)
	raw, err := base64.StdEncoding.DecodeString(data)
	if err == nil {
		return raw, nil
	}
	if strings.Contains(data, "library ") || strings.Contains(data, "define ") {
		return []byte(data), nil
	}
	return nil, errf("decode Library.content.data: %w", err)
}

func compileLibrarySource(engine *Engine, src, url, name, version string) (*Library, error) {
	if engine == nil {
		return nil, ErrEngineUnavailable
	}
	lib, err := engine.ParseLibrary(src)
	if err != nil {
		return nil, err
	}
	if url != "" {
		lib.URL = url
	}
	if name != "" && lib.Name == "" {
		lib.Name = name
	}
	if version != "" && lib.Version == "" {
		lib.Version = version
	}
	return lib, nil
}

func splitCanonical(canonical string) (url, version string) {
	parts := strings.SplitN(canonical, "|", 2)
	url = parts[0]
	if len(parts) == 2 {
		version = parts[1]
	}
	return url, version
}

func matchesCanonical(lib *Library, canonical string) bool {
	if lib == nil || canonical == "" {
		return false
	}
	if lib.URL == canonical {
		return true
	}
	url, version := splitCanonical(canonical)
	if lib.URL == url && (version == "" || lib.Version == version) {
		return true
	}
	if lib.Name == url && (version == "" || lib.Version == version) {
		return true
	}
	if lib.URL != "" && version != "" && lib.URL+"|"+lib.Version == canonical {
		return true
	}
	return false
}
