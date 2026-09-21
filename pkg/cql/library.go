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
	src, elm, err := loadLibraryPayload(lib)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(src) != "" {
		return src, nil
	}
	if len(elm) > 0 {
		return "", errf("%w: Library %q is ELM-only; CQL source (text/cql) is required", ErrUnsupported, libraryLabel(lib))
	}
	return "", errf("%w: Library %q has no text/cql content", ErrLibraryNotFound, libraryLabel(lib))
}

func loadLibraryPayload(lib fhirLibrary) (source string, elm []byte, err error) {
	var elmOnly bool
	var elmXML bool
	for _, c := range lib.Content {
		ct := strings.ToLower(strings.TrimSpace(c.ContentType))
		if isCQLContentType(ct) {
			if strings.TrimSpace(c.Data) == "" {
				continue
			}
			raw, decErr := decodeLibraryData(c.Data)
			if decErr != nil {
				return "", nil, decErr
			}
			src := strings.TrimSpace(string(raw))
			if src == "" {
				continue
			}
			return src, nil, nil
		}
		if !isELMContentType(ct) {
			continue
		}
		elmOnly = true
		if strings.Contains(ct, "xml") {
			elmXML = true
		}
		if strings.TrimSpace(c.Data) == "" {
			continue
		}
		raw, decErr := decodeLibraryData(c.Data)
		if decErr != nil {
			return "", nil, decErr
		}
		if src := recoverCQLFromELM(raw); src != "" {
			return src, raw, nil
		}
		if looksLikeJSONObject(raw) {
			elm = raw
			elmXML = false
		}
	}
	if len(elm) > 0 {
		return "", elm, nil
	}
	if elmXML {
		return "", nil, errf("%w: Library %q has ELM XML; application/elm+json is required", ErrUnsupported, libraryLabel(lib))
	}
	if elmOnly {
		return "", nil, errf("%w: Library %q is ELM-only but ELM payload is empty", ErrUnsupported, libraryLabel(lib))
	}
	return "", nil, errf("%w: Library %q has no text/cql content", ErrLibraryNotFound, libraryLabel(lib))
}

func isELMContentType(ct string) bool {
	return strings.Contains(ct, "elm")
}

func looksLikeJSONObject(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")
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
	if strings.Contains(data, "library ") || strings.Contains(data, "define ") || looksLikeJSONObject([]byte(data)) {
		return []byte(data), nil
	}
	return nil, errf("decode Library.content.data: %w", err)
}

// AttachLibraryCQL returns a copy of env with text/cql content set to source.
// Existing CQL attachments are replaced; ELM and other content attachments are kept.
func AttachLibraryCQL(env *types.ResourceEnvelope, source string) (*types.ResourceEnvelope, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, ErrEmptyExpression
	}
	if env == nil || len(env.JSON) == 0 {
		return nil, errf("CQL Library envelope is empty")
	}
	var obj map[string]any
	if err := json.Unmarshal(env.JSON, &obj); err != nil {
		return nil, errf("decode CQL Library: %w", err)
	}
	if rt, _ := obj["resourceType"].(string); rt != "" && rt != "Library" {
		return nil, errf("expected Library resource, got %s", rt)
	}
	obj["resourceType"] = "Library"
	encoded := base64.StdEncoding.EncodeToString([]byte(source))
	var content []any
	if existing, ok := obj["content"].([]any); ok {
		content = existing
	}
	replaced := false
	out := make([]any, 0, len(content)+1)
	for _, item := range content {
		m, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		ct, _ := m["contentType"].(string)
		if isCQLContentType(strings.ToLower(strings.TrimSpace(ct))) {
			m["contentType"] = "text/cql"
			m["data"] = encoded
			replaced = true
		}
		out = append(out, m)
	}
	if !replaced {
		out = append([]any{map[string]any{"contentType": "text/cql", "data": encoded}}, out...)
	}
	obj["content"] = out
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, errf("encode CQL Library: %w", err)
	}
	return types.NewJSONCodec().ParseJSON("Library", raw)
}

func compileLibraryJSON(engine *Engine, data []byte) (*Library, error) {
	if engine == nil {
		return nil, ErrEngineUnavailable
	}
	if len(data) == 0 {
		return nil, errf("CQL Library envelope is empty")
	}
	var lib fhirLibrary
	if err := json.Unmarshal(data, &lib); err != nil {
		return nil, errf("decode CQL Library: %w", err)
	}
	if lib.ResourceType != "" && lib.ResourceType != "Library" {
		return nil, errf("expected Library resource, got %s", lib.ResourceType)
	}
	src, elm, err := loadLibraryPayload(lib)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(src) != "" {
		return compileLibrarySource(engine, src, lib.URL, lib.Name, lib.Version)
	}
	if len(elm) > 0 {
		compiled, err := engine.ParseELM(elm)
		if err != nil {
			return nil, err
		}
		return applyLibraryMeta(compiled, lib.URL, lib.Name, lib.Version), nil
	}
	return nil, errf("%w: Library %q has no text/cql or ELM content", ErrLibraryNotFound, libraryLabel(lib))
}

func compileLibrarySource(engine *Engine, src, url, name, version string) (*Library, error) {
	if engine == nil {
		return nil, ErrEngineUnavailable
	}
	lib, err := engine.ParseLibrary(src)
	if err != nil {
		return nil, err
	}
	return applyLibraryMeta(lib, url, name, version), nil
}

func applyLibraryMeta(lib *Library, url, name, version string) *Library {
	if lib == nil {
		return nil
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
	return lib
}

// CompileLibrary compiles a FHIR Library resource from text/cql or application/elm+json.
func (e *Engine) CompileLibrary(env *types.ResourceEnvelope) (*Library, error) {
	if e == nil {
		return nil, ErrEngineUnavailable
	}
	return compileEnvelope(e, env)
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
