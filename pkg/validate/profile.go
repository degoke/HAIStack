package validate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ProfileCatalog looks up compiled StructureDefinitions by canonical URL.
type ProfileCatalog interface {
	GetStructureDefinition(canonicalURL string) (*StructureDefinition, bool)
}

// ElementConstraint is a FHIRPath invariant from a StructureDefinition element.
type ElementConstraint struct {
	Key        string
	Severity   string
	Expression string
	Human      string
	compiled   fhirpath.CompiledExpression
	compileMu  sync.Mutex
}

// ElementBinding is a terminology binding from a StructureDefinition element.
type ElementBinding struct {
	Strength string
	ValueSet string
	Version  string
}

// ElementSlicing describes how a repeating element is sliced.
type ElementSlicing struct {
	Discriminators []SliceDiscriminator
	Rules          string
}

// SliceDiscriminator matches array items to named slices.
type SliceDiscriminator struct {
	Type string
	Path string
}

// ElementDefinition is one snapshot or differential element.
type ElementDefinition struct {
	Path        string
	Min         int
	Max         string
	Types       []string
	Constraints []ElementConstraint
	Binding     *ElementBinding
	Slicing     *ElementSlicing
	SliceName   string
	Pattern     map[string]interface{}
}

// StructureDefinition is the subset of a FHIR StructureDefinition used for
// profile validation on write.
type StructureDefinition struct {
	URL          string
	Type         string
	Kind         string
	Derivation   string
	UseSnapshot  bool
	Elements     []ElementDefinition
	allowedChild map[string]map[string]struct{}
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
		if !isStructureDefinitionJSON(raw) {
			continue
		}
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
	Derivation   string `json:"derivation"`
	Snapshot     *struct {
		Element []elementDefinitionJSON `json:"element"`
	} `json:"snapshot"`
	Differential *struct {
		Element []elementDefinitionJSON `json:"element"`
	} `json:"differential"`
}

func isStructureDefinitionJSON(raw []byte) bool {
	var peek struct {
		ResourceType string `json:"resourceType"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return false
	}
	return peek.ResourceType == "StructureDefinition"
}

type elementDefinitionJSON struct {
	Path string `json:"path"`
	Min  *int   `json:"min"`
	Max  string `json:"max"`
	Type []struct {
		Code string `json:"code"`
	} `json:"type"`
	Constraint []struct {
		Key        string `json:"key"`
		Severity   string `json:"severity"`
		Expression string `json:"expression"`
		Human      string `json:"human"`
	} `json:"constraint"`
	Binding *struct {
		Strength string `json:"strength"`
		ValueSet string `json:"valueSet"`
	} `json:"binding"`
	Slicing *struct {
		Discriminator []struct {
			Type string `json:"type"`
			Path string `json:"path"`
		} `json:"discriminator"`
		Rules string `json:"rules"`
	} `json:"slicing"`
	Pattern json.RawMessage `json:"pattern"`
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
		URL:        env.URL,
		Type:       env.Type,
		Kind:       env.Kind,
		Derivation: env.Derivation,
	}
	var src *struct {
		Element []elementDefinitionJSON `json:"element"`
	}
	switch {
	case env.Derivation == "constraint" && env.Differential != nil && len(env.Differential.Element) > 0:
		src = env.Differential
		sd.UseSnapshot = false
	case env.Snapshot != nil && len(env.Snapshot.Element) > 0:
		src = env.Snapshot
		sd.UseSnapshot = true
	case env.Differential != nil && len(env.Differential.Element) > 0:
		src = env.Differential
		sd.UseSnapshot = false
	default:
		src = nil
	}
	if src != nil {
		for _, el := range src.Element {
			min := 0
			if el.Min != nil {
				min = *el.Min
			}
			parsed := ElementDefinition{
				Path: el.Path,
				Min:  min,
				Max:  el.Max,
			}
			for _, typ := range el.Type {
				if typ.Code != "" {
					parsed.Types = append(parsed.Types, typ.Code)
				}
			}
			for _, c := range el.Constraint {
				parsed.Constraints = append(parsed.Constraints, ElementConstraint{
					Key:        c.Key,
					Severity:   c.Severity,
					Expression: c.Expression,
					Human:      c.Human,
				})
			}
			if el.Binding != nil && el.Binding.ValueSet != "" {
				url, version := splitValueSetCanonical(el.Binding.ValueSet)
				parsed.Binding = &ElementBinding{
					Strength: el.Binding.Strength,
					ValueSet: url,
					Version:  version,
				}
			}
			if el.Slicing != nil {
				slicing := &ElementSlicing{Rules: el.Slicing.Rules}
				for _, d := range el.Slicing.Discriminator {
					slicing.Discriminators = append(slicing.Discriminators, SliceDiscriminator{
						Type: d.Type,
						Path: d.Path,
					})
				}
				parsed.Slicing = slicing
			}
			if len(el.Pattern) > 0 && string(el.Pattern) != "null" {
				var pattern map[string]interface{}
				if err := json.Unmarshal(el.Pattern, &pattern); err == nil && len(pattern) > 0 {
					parsed.Pattern = pattern
				}
			}
			if i := strings.Index(parsed.Path, ":"); i >= 0 {
				parsed.SliceName = parsed.Path[i+1:]
			}
			sd.Elements = append(sd.Elements, parsed)
		}
	}
	return sd, true, nil
}

func splitValueSetCanonical(canonical string) (url, version string) {
	if i := strings.Index(canonical, "|"); i >= 0 {
		return canonical[:i], canonical[i+1:]
	}
	return canonical, ""
}

func profileValidationFull(opts ValidateOptions) bool {
	return opts.Mode == ValidationModeFull
}

func (e *builtinEngine) validateProfiles(ctx context.Context, res *types.ResourceEnvelope, obj map[string]interface{}, resourceType string, opts ValidateOptions, issues *[]ValidationIssue) {
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
	if opts.EnforceBaseProfile && resourceType != "" {
		appendURL(BaseStructureDefinitionURL(resourceType))
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

	evaluatedConstraints := make(map[string]struct{})
	for _, url := range urls {
		if err := ctx.Err(); err != nil {
			return
		}
		sd, err := lookupStructureDefinition(catalog, url)
		if err != nil {
			if errors.Is(err, ErrProfileNotFound) {
				*issues = append(*issues, issue(
					"unknown-profile",
					fmt.Sprintf("profile %q is not installed", url),
					[]string{"Resource.meta.profile"},
				))
			} else {
				*issues = append(*issues, issue(
					"profile-parse",
					fmt.Sprintf("profile %q could not be parsed: %v", url, err),
					[]string{"Resource.meta.profile"},
				))
			}
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
		applyProfileValidation(ctx, res, obj, sd, e.fhirpath, opts, evaluatedConstraints, issues)
	}
}

func applyProfileValidation(ctx context.Context, res *types.ResourceEnvelope, obj map[string]interface{}, sd *StructureDefinition, engine fhirpath.Engine, opts ValidateOptions, evaluatedConstraints map[string]struct{}, issues *[]ValidationIssue) {
	full := profileValidationFull(opts)
	if sd.UseSnapshot {
		validateProfileSnapshotStructure(obj, sd, issues)
	} else {
		validateProfileSlicing(obj, sd, issues)
	}
	if sd.UseSnapshot {
		if opts.ProfileConstraints && engine != nil {
			ensureConstraintsCompiled(sd, engine)
			validateProfileConstraints(ctx, res, sd, engine, evaluatedConstraints, issues)
		}
		if full {
			validateExtensionPolicy(obj, sd, issues)
		}
	}
	if full && opts.Terminology != nil {
		validateProfileTerminology(ctx, obj, sd, opts, issues)
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
		parent, _ := splitElementPath(el.Path)
		if parent != sd.Type && parent != "" {
			if countPath(obj, parent) == 0 {
				continue
			}
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

func (sd *StructureDefinition) buildAllowedChildren() {
	if sd.allowedChild != nil {
		return
	}
	sd.allowedChild = make(map[string]map[string]struct{})
	for _, el := range sd.Elements {
		if el.Path == "" || el.Path == sd.Type {
			continue
		}
		parent, child := splitElementPath(el.Path)
		if parent == "" || child == "" {
			continue
		}
		if strings.Contains(child, "[x]") {
			for _, key := range choiceJSONKeys(child, el.Types) {
				addAllowedChild(sd.allowedChild, parent, key)
			}
			continue
		}
		if strings.Contains(child, ":") {
			continue
		}
		addAllowedChild(sd.allowedChild, parent, child)
	}
}

func addAllowedChild(index map[string]map[string]struct{}, parent, child string) {
	if index[parent] == nil {
		index[parent] = make(map[string]struct{})
	}
	index[parent][child] = struct{}{}
}

func splitElementPath(path string) (parent, child string) {
	i := strings.LastIndex(path, ".")
	if i < 0 {
		return path, ""
	}
	return path[:i], path[i+1:]
}

func choiceJSONKeys(choiceName string, types []string) []string {
	base := strings.TrimSuffix(choiceName, "[x]")
	if base == "" || len(types) == 0 {
		return nil
	}
	out := make([]string, 0, len(types))
	for _, typ := range types {
		runes := []rune(typ)
		runes[0] = unicode.ToUpper(runes[0])
		out = append(out, base+string(runes))
	}
	return out
}

var backboneAllowlist = map[string]struct{}{
	"resourceType":      {},
	"id":                {},
	"meta":              {},
	"implicitRules":     {},
	"language":          {},
	"text":              {},
	"contained":         {},
	"extension":         {},
	"modifierExtension": {},
}

func isAlwaysAllowedKey(key string) bool {
	_, ok := backboneAllowlist[key]
	return ok
}

func validateUnknownElements(obj map[string]interface{}, sd *StructureDefinition, issues *[]ValidationIssue) {
	sd.buildAllowedChildren()
	walkUnknownElements(obj, sd.Type, sd, issues)
}

var metaFieldAllowlist = map[string]struct{}{
	"profile":     {},
	"version":     {},
	"lastUpdated": {},
	"source":      {},
	"security":    {},
	"tag":         {},
}

func isMetaElementPath(path string) bool {
	return strings.HasSuffix(path, ".meta")
}

func walkUnknownElements(node interface{}, path string, sd *StructureDefinition, issues *[]ValidationIssue) {
	switch current := node.(type) {
	case map[string]interface{}:
		if isMetaElementPath(path) {
			for key, value := range current {
				if _, ok := metaFieldAllowlist[key]; !ok {
					*issues = append(*issues, issue(
						"unknown-element",
						fmt.Sprintf("element %q is not allowed at %s (%s)", key, path, sd.URL),
						[]string{path + "." + key},
					))
					continue
				}
				walkUnknownElements(value, path+"."+key, sd, issues)
			}
			return
		}
		allowed := sd.allowedChild[path]
		for key, value := range current {
			if isAlwaysAllowedKey(key) {
				nextPath := path
				if key == "meta" || key == "text" {
					if path == "" {
						nextPath = key
					} else {
						nextPath = path + "." + key
					}
				}
				if key != "extension" && key != "modifierExtension" {
					walkUnknownElements(value, nextPath, sd, issues)
				}
				continue
			}
			if allowed == nil {
				continue
			}
			if _, ok := allowed[key]; !ok {
				*issues = append(*issues, issue(
					"unknown-element",
					fmt.Sprintf("element %q is not allowed at %s (%s)", key, path, sd.URL),
					[]string{path + "." + key},
				))
				continue
			}
			nextPath := elementPathForJSONKey(sd, path, key)
			walkUnknownElements(value, nextPath, sd, issues)
		}
	case []interface{}:
		for _, item := range current {
			walkUnknownElements(item, path, sd, issues)
		}
	}
}

func elementPathForJSONKey(sd *StructureDefinition, parentPath, jsonKey string) string {
	for _, el := range sd.Elements {
		p, child := splitElementPath(el.Path)
		if p != parentPath {
			continue
		}
		if child == jsonKey {
			return el.Path
		}
		for _, choiceKey := range choiceJSONKeys(child, el.Types) {
			if choiceKey == jsonKey {
				return el.Path
			}
		}
	}
	return parentPath + "." + jsonKey
}

func ensureConstraintsCompiled(sd *StructureDefinition, engine fhirpath.Engine) {
	if sd == nil || engine == nil {
		return
	}
	for i := range sd.Elements {
		for j := range sd.Elements[i].Constraints {
			c := &sd.Elements[i].Constraints[j]
			if c.Expression == "" {
				continue
			}
			c.compileMu.Lock()
			if c.compiled == nil {
				if compiled, err := engine.Compile(c.Expression); err == nil {
					c.compiled = compiled
				}
			}
			c.compileMu.Unlock()
		}
	}
}

func validateProfileConstraints(ctx context.Context, res *types.ResourceEnvelope, sd *StructureDefinition, engine fhirpath.Engine, evaluatedKeys map[string]struct{}, issues *[]ValidationIssue) {
	if res == nil {
		return
	}
	for _, el := range sd.Elements {
		for _, c := range el.Constraints {
			if c.Expression == "" {
				continue
			}
			if _, ok := evaluatedKeys[c.Key]; ok {
				continue
			}
			evaluatedKeys[c.Key] = struct{}{}
			var (
				ok  bool
				err error
			)
			if c.compiled != nil {
				ok, err = c.compiled.EvalBool(ctx, res)
			} else {
				ok, err = engine.EvalBool(ctx, c.Expression, res)
			}
			if err != nil {
				*issues = append(*issues, ValidationIssue{
					Severity:    "warning",
					Code:        "invariant-evaluation",
					Diagnostics: fmt.Sprintf("%s: %v", c.Key, err),
					Expression:  []string{el.Path},
				})
				continue
			}
			if ok {
				continue
			}
			severity := strings.ToLower(c.Severity)
			if severity == "" {
				severity = "error"
			}
			diagnostics := c.Human
			if diagnostics == "" {
				diagnostics = c.Key + ": " + c.Expression
			}
			*issues = append(*issues, ValidationIssue{
				Severity:    severity,
				Code:        "invariant",
				Diagnostics: diagnostics,
				Expression:  []string{el.Path},
			})
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
