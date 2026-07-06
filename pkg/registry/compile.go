package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// ParsedDefinition is normalized metadata extracted from one definition JSON resource.
type ParsedDefinition struct {
	CanonicalURL     string
	Version          string
	FHIRVersion      string
	FHIRResourceType string
	DefinitionKind   store.DefinitionKind
	Name             string
	Status           string
}

const (
	targetRoleDefines    = "defines"
	targetRoleSearchBase = "search-base"
)

// ParseDefinition extracts catalog metadata and target mappings from raw JSON.
func ParseDefinition(jsonData []byte) (ParsedDefinition, []store.DefinitionTargetRecord, error) {
	var envelope struct {
		ResourceType string   `json:"resourceType"`
		URL          string   `json:"url"`
		Version      string   `json:"version"`
		FHIRVersion  string   `json:"fhirVersion"`
		Name         string   `json:"name"`
		Status       string   `json:"status"`
		Kind         string   `json:"kind"`
		Type         string   `json:"type"`
		Code         string   `json:"code"`
		Base         []string `json:"base"`
	}
	if err := json.Unmarshal(jsonData, &envelope); err != nil {
		return ParsedDefinition{}, nil, fmt.Errorf("%w: decode json: %v", ErrInvalidDefinition, err)
	}
	if envelope.URL == "" {
		return ParsedDefinition{}, nil, fmt.Errorf("%w: missing url", ErrInvalidDefinition)
	}
	if envelope.Version == "" {
		envelope.Version = defaultFHIRVersion
	}
	if envelope.FHIRVersion == "" {
		envelope.FHIRVersion = defaultFHIRVersion
	}

	parsed := ParsedDefinition{
		CanonicalURL:     envelope.URL,
		Version:          envelope.Version,
		FHIRVersion:      envelope.FHIRVersion,
		FHIRResourceType: envelope.ResourceType,
		Name:             envelope.Name,
		Status:           envelope.Status,
	}

	switch envelope.ResourceType {
	case "StructureDefinition":
		parsed.DefinitionKind = store.DefinitionKindStructureDefinition
		if envelope.Kind == "resource" && envelope.Type != "" {
			targets := []store.DefinitionTargetRecord{{
				CanonicalURL:       envelope.URL,
				Version:            envelope.Version,
				TargetResourceType: envelope.Type,
				TargetRole:         targetRoleDefines,
			}}
			return parsed, targets, nil
		}
		return parsed, nil, nil
	case "SearchParameter":
		parsed.DefinitionKind = store.DefinitionKindSearchParameter
		if len(envelope.Base) == 0 {
			return ParsedDefinition{}, nil, fmt.Errorf("%w: search parameter missing base", ErrInvalidDefinition)
		}
		targets := make([]store.DefinitionTargetRecord, 0, len(envelope.Base))
		for _, base := range envelope.Base {
			targets = append(targets, store.DefinitionTargetRecord{
				CanonicalURL:       envelope.URL,
				Version:            envelope.Version,
				TargetResourceType: base,
				TargetRole:         targetRoleSearchBase,
			})
		}
		return parsed, targets, nil
	default:
		parsed.DefinitionKind = store.DefinitionKindFromResourceType(envelope.ResourceType)
		return parsed, nil, nil
	}
}

// CompileSnapshot builds an immutable registry view from persisted catalog state.
func CompileSnapshot(
	ctx context.Context,
	definitions store.DefinitionStore,
	installs store.RegistryInstallStore,
	fhirVersion string,
	now func() time.Time,
) (*Snapshot, error) {
	if fhirVersion == "" {
		fhirVersion = defaultFHIRVersion
	}
	if now == nil {
		now = time.Now
	}

	allDefinitions, err := definitions.List(ctx, store.DefinitionFilter{FHIRVersion: fhirVersion})
	if err != nil {
		return nil, err
	}
	enabledRows, err := installs.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}

	canonical := make(map[string]map[string][]byte)
	structureByType := make(map[string][]byte)
	searchByType := make(map[string]map[string]SearchParameterInfo)

	for _, record := range allDefinitions {
		if canonical[record.CanonicalURL] == nil {
			canonical[record.CanonicalURL] = make(map[string][]byte)
		}
		canonical[record.CanonicalURL][record.Version] = append([]byte(nil), record.JSONData...)

		switch record.DefinitionKind {
		case store.DefinitionKindStructureDefinition:
			_, extractedTargets, err := ParseDefinition(record.JSONData)
			if err != nil {
				return nil, fmt.Errorf("%w: structure definition %s: %v", ErrSnapshotCompile, record.CanonicalURL, err)
			}
			for _, target := range extractedTargets {
				if target.TargetRole == targetRoleDefines {
					structureByType[target.TargetResourceType] = append([]byte(nil), record.JSONData...)
				}
			}
		case store.DefinitionKindSearchParameter:
			info, err := parseSearchParameterInfo(record.JSONData, record.CanonicalURL, record.Version)
			if err != nil {
				return nil, fmt.Errorf("%w: search parameter %s: %v", ErrSnapshotCompile, record.CanonicalURL, err)
			}
			_, extractedTargets, err := ParseDefinition(record.JSONData)
			if err != nil {
				return nil, err
			}
			for _, target := range extractedTargets {
				if searchByType[target.TargetResourceType] == nil {
					searchByType[target.TargetResourceType] = make(map[string]SearchParameterInfo)
				}
				searchByType[target.TargetResourceType][info.Code] = info
			}
		}
	}

	enabled := make(map[string]struct{})
	for _, row := range enabledRows {
		if row.DefinitionKind != store.DefinitionKindStructureDefinition {
			continue
		}
		if _, ok := structureByType[row.TargetResourceType]; !ok {
			return nil, fmt.Errorf("%w: enabled resource %s missing structure definition", ErrSnapshotCompile, row.TargetResourceType)
		}
		enabled[row.TargetResourceType] = struct{}{}
	}

	filteredSearch := make(map[string]map[string]SearchParameterInfo)
	for resourceType := range enabled {
		if params, ok := searchByType[resourceType]; ok {
			filteredSearch[resourceType] = params
		}
	}

	return &Snapshot{
		fhirVersion:     fhirVersion,
		enabled:         enabled,
		structureByType: structureByType,
		searchByType:    filteredSearch,
		canonical:       canonical,
		profilesByType:  make(map[string][]DefinitionRef),
		operations:      nil,
		compiledAt:      now().UTC(),
	}, nil
}

func parseSearchParameterInfo(jsonData []byte, canonicalURL, version string) (SearchParameterInfo, error) {
	var sp struct {
		Code       string   `json:"code"`
		Name       string   `json:"name"`
		Type       string   `json:"type"`
		Expression string   `json:"expression"`
		Target     []string `json:"target"`
		Component  []struct {
			Definition string `json:"definition"`
			Expression string `json:"expression"`
		} `json:"component"`
	}
	if err := json.Unmarshal(jsonData, &sp); err != nil {
		return SearchParameterInfo{}, err
	}
	if sp.Code == "" {
		return SearchParameterInfo{}, fmt.Errorf("missing code")
	}
	components := make([]CompositeComponent, 0, len(sp.Component))
	for _, c := range sp.Component {
		components = append(components, CompositeComponent{
			Definition: c.Definition,
			Expression: c.Expression,
		})
	}
	return SearchParameterInfo{
		CanonicalURL: canonicalURL,
		Version:      version,
		Code:         sp.Code,
		Name:         sp.Name,
		Type:         sp.Type,
		Expression:   sp.Expression,
		Target:       append([]string(nil), sp.Target...),
		Component:    components,
	}, nil
}

// Snapshot is an immutable compiled registry view.
type Snapshot struct {
	fhirVersion     string
	enabled         map[string]struct{}
	structureByType map[string][]byte
	searchByType    map[string]map[string]SearchParameterInfo
	canonical       map[string]map[string][]byte
	profilesByType  map[string][]DefinitionRef
	operations      []DefinitionRef
	compiledAt      time.Time
}

// ProfilesFor returns installed profile references for resourceType.
// Reserved for future profile compilation; empty in the MVP snapshot.
func (s *Snapshot) ProfilesFor(resourceType string) []DefinitionRef {
	if s == nil {
		return nil
	}
	return append([]DefinitionRef(nil), s.profilesByType[resourceType]...)
}

// Operations returns installed operation definition references.
// Reserved for future operation compilation; empty in the MVP snapshot.
func (s *Snapshot) Operations() []DefinitionRef {
	if s == nil {
		return nil
	}
	return append([]DefinitionRef(nil), s.operations...)
}

// IsResourceEnabled reports whether resourceType is enabled in the compiled snapshot.
func (s *Snapshot) IsResourceEnabled(resourceType string) bool {
	if s == nil {
		return false
	}
	_, ok := s.enabled[resourceType]
	return ok
}

// IsInstalled implements validate.ResourceTypeRegistry.
func (s *Snapshot) IsInstalled(resourceType string) bool {
	return s.IsResourceEnabled(resourceType)
}

// StructureDefinitionFor returns the compiled base StructureDefinition JSON for resourceType.
func (s *Snapshot) StructureDefinitionFor(resourceType string) ([]byte, bool) {
	if s == nil {
		return nil, false
	}
	data, ok := s.structureByType[resourceType]
	return data, ok
}

// SearchParametersFor returns installed search parameters for an enabled resource type.
func (s *Snapshot) SearchParametersFor(resourceType string) []SearchParameterInfo {
	if s == nil || !s.IsResourceEnabled(resourceType) {
		return nil
	}
	params := s.searchByType[resourceType]
	out := make([]SearchParameterInfo, 0, len(params))
	for _, info := range params {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// SearchParameter returns one search parameter for resourceType and code.
func (s *Snapshot) SearchParameter(resourceType, code string) (*SearchParameterInfo, bool) {
	if s == nil || !s.IsResourceEnabled(resourceType) {
		return nil, false
	}
	params, ok := s.searchByType[resourceType]
	if !ok {
		return nil, false
	}
	info, ok := params[code]
	if !ok {
		return nil, false
	}
	copyInfo := info
	return &copyInfo, true
}

// SearchExpression returns the FHIRPath search expression for resourceType and code.
func (s *Snapshot) SearchExpression(resourceType, code string) (string, bool) {
	info, ok := s.SearchParameter(resourceType, code)
	if !ok {
		return "", false
	}
	return info.Expression, true
}

// DefinitionsByCanonical returns raw JSON for canonicalURL and version.
func (s *Snapshot) DefinitionsByCanonical(canonicalURL, version string) ([]byte, bool) {
	if s == nil {
		return nil, false
	}
	versions, ok := s.canonical[canonicalURL]
	if !ok {
		return nil, false
	}
	data, ok := versions[version]
	return data, ok
}

// CapabilitySnapshot returns a lightweight capability view for runtime consumers.
func (s *Snapshot) CapabilitySnapshot() CapabilitySnapshot {
	if s == nil {
		return CapabilitySnapshot{}
	}
	resourceTypes := make([]string, 0, len(s.enabled))
	for resourceType := range s.enabled {
		resourceTypes = append(resourceTypes, resourceType)
	}
	sort.Strings(resourceTypes)

	resources := make([]ResourceCapability, 0, len(resourceTypes))
	for _, resourceType := range resourceTypes {
		capability := ResourceCapability{
			ResourceType:     resourceType,
			SearchParameters: s.SearchParametersFor(resourceType),
		}
		if data, ok := s.structureByType[resourceType]; ok {
			parsed, _, err := ParseDefinition(data)
			if err == nil {
				capability.StructureDefinition = &DefinitionRef{
					CanonicalURL: parsed.CanonicalURL,
					Version:      parsed.Version,
				}
			}
		}
		resources = append(resources, capability)
	}

	return CapabilitySnapshot{
		FHIRVersion: s.fhirVersion,
		Resources:   resources,
		CompiledAt:  s.compiledAt,
	}
}

// loadR4Bundle reads embedded base R4 definitions.
func loadR4Bundle() ([][]byte, error) {
	var out [][]byte
	err := fs.WalkDir(r4BundleFS, "internal/bundles/r4", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := r4BundleFS.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, data)
		return nil
	})
	return out, err
}
