package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProfileCatalog looks up compiled StructureDefinitions by canonical URL.
type ProfileCatalog interface {
	GetStructureDefinition(canonicalURL string) (*StructureDefinition, bool)
}

// StructureDefinition is the subset of a FHIR StructureDefinition needed for
// cardinality enforcement on write.
type StructureDefinition struct {
	URL      string
	Type     string
	Kind     string
	Elements []ElementDefinition
}

// ElementDefinition is one snapshot or differential element.
type ElementDefinition struct {
	Path string
	Min  int
	Max  string
}

// MemoryProfileCatalog is an in-memory ProfileCatalog.
type MemoryProfileCatalog map[string]*StructureDefinition

// GetStructureDefinition implements ProfileCatalog.
func (c MemoryProfileCatalog) GetStructureDefinition(canonicalURL string) (*StructureDefinition, bool) {
	if c == nil {
		return nil, false
	}
	sd, ok := c[canonicalURL]
	return sd, ok
}

// LoadProfileCatalogFromJSON parses StructureDefinition resources.
func LoadProfileCatalogFromJSON(resources [][]byte) (MemoryProfileCatalog, error) {
	catalog := make(MemoryProfileCatalog)
	for _, raw := range resources {
		sd, ok, err := parseStructureDefinition(raw)
		if err != nil {
			return nil, err
		}
		if !ok || sd.URL == "" {
			continue
		}
		catalog[sd.URL] = sd
	}
	return catalog, nil
}

// LoadProfileCatalogFromDir loads StructureDefinitions from a directory of FHIR JSON.
func LoadProfileCatalogFromDir(dir string) (MemoryProfileCatalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read profile directory: %w", err)
	}
	var resources [][]byte
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		resources = append(resources, data)
	}
	return LoadProfileCatalogFromJSON(resources)
}

type structureDefinitionJSON struct {
	ResourceType string `json:"resourceType"`
	URL          string `json:"url"`
	Type         string `json:"type"`
	Kind         string `json:"kind"`
	Snapshot     *struct {
		Element []elementDefinitionJSON `json:"element"`
	} `json:"snapshot"`
	Differential *struct {
		Element []elementDefinitionJSON `json:"element"`
	} `json:"differential"`
}

type elementDefinitionJSON struct {
	Path string `json:"path"`
	Min  *int   `json:"min"`
	Max  string `json:"max"`
}

func parseStructureDefinition(raw []byte) (*StructureDefinition, bool, error) {
	var env structureDefinitionJSON
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, false, fmt.Errorf("decode StructureDefinition: %w", err)
	}
	if env.ResourceType != "StructureDefinition" {
		return nil, false, nil
	}
	sd := &StructureDefinition{
		URL:  env.URL,
		Type: env.Type,
		Kind: env.Kind,
	}
	src := env.Snapshot
	if env.Differential != nil && len(env.Differential.Element) > 0 {
		src = env.Differential
	}
	if src != nil {
		for _, el := range src.Element {
			min := 0
			if el.Min != nil {
				min = *el.Min
			}
			sd.Elements = append(sd.Elements, ElementDefinition{
				Path: el.Path,
				Min:  min,
				Max:  el.Max,
			})
		}
	}
	return sd, true, nil
}

func (e *builtinEngine) validateProfiles(ctx context.Context, obj map[string]interface{}, resourceType string, opts ValidateOptions, issues *[]ValidationIssue) {
	if err := ctx.Err(); err != nil {
		return
	}
	catalog := opts.ProfileCatalog
	if catalog == nil {
		catalog = e.profileCatalog
	}
	if catalog == nil {
		return
	}

	seen := make(map[string]struct{})
	var urls []string
	appendURL := func(url string) {
		url = strings.TrimSpace(url)
		if url == "" {
			return
		}
		if _, ok := seen[url]; ok {
			return
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	for _, url := range opts.Profiles {
		appendURL(url)
	}
	if opts.EnforceDeclaredProfiles {
		for _, url := range metaProfiles(obj) {
			appendURL(url)
		}
	}
	if len(urls) == 0 {
		return
	}

	for _, url := range urls {
		if err := ctx.Err(); err != nil {
			return
		}
		sd, ok := catalog.GetStructureDefinition(url)
		if !ok {
			*issues = append(*issues, issue(
				"unknown-profile",
				fmt.Sprintf("profile %q is not installed", url),
				[]string{"Resource.meta.profile"},
			))
			continue
		}
		if sd.Type != "" && resourceType != "" && sd.Type != resourceType {
			*issues = append(*issues, issue(
				"profile",
				fmt.Sprintf("profile %s applies to %s, not %s", url, sd.Type, resourceType),
				[]string{"Resource.meta.profile"},
			))
			continue
		}
		validateProfileCardinality(obj, sd, issues)
	}
}

func metaProfiles(obj map[string]interface{}) []string {
	meta, _ := obj["meta"].(map[string]interface{})
	if meta == nil {
		return nil
	}
	raw, ok := meta["profile"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), v...)
	case string:
		return []string{v}
	default:
		return nil
	}
}

func validateProfileCardinality(obj map[string]interface{}, sd *StructureDefinition, issues *[]ValidationIssue) {
	for _, el := range sd.Elements {
		if el.Path == "" || el.Path == sd.Type {
			continue
		}
		if strings.Contains(el.Path, "[x]") || strings.Contains(el.Path, ":") {
			continue
		}
		count := countPath(obj, el.Path)
		if el.Min > 0 && count < el.Min {
			*issues = append(*issues, issue(
				"required",
				fmt.Sprintf("%s: minimum required = %d, but only found %d (%s)", el.Path, el.Min, count, sd.URL),
				[]string{el.Path},
			))
		}
		if max, ok := parseMax(el.Max); ok && count > max {
			*issues = append(*issues, issue(
				"structure",
				fmt.Sprintf("%s: max allowed = %d, but found %d (%s)", el.Path, max, count, sd.URL),
				[]string{el.Path},
			))
		}
	}
}

func parseMax(max string) (int, bool) {
	switch max {
	case "", "*":
		return 0, false
	default:
		n, err := strconv.Atoi(max)
		if err != nil {
			return 0, false
		}
		return n, true
	}
}

func countPath(obj map[string]interface{}, path string) int {
	segments := strings.Split(path, ".")
	if len(segments) < 2 {
		return 0
	}
	return countSegments(obj, segments[1:])
}

func countSegments(node interface{}, segments []string) int {
	if node == nil || len(segments) == 0 {
		if node == nil {
			return 0
		}
		return 1
	}
	switch current := node.(type) {
	case []interface{}:
		total := 0
		for _, item := range current {
			total += countSegments(item, segments)
		}
		return total
	case map[string]interface{}:
		next, ok := current[segments[0]]
		if !ok || next == nil {
			return 0
		}
		if len(segments) == 1 {
			if arr, ok := next.([]interface{}); ok {
				return len(arr)
			}
			return 1
		}
		return countSegments(next, segments[1:])
	default:
		return 0
	}
}
