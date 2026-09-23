package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/haistack/pkg/fhirpath"
	"github.com/degoke/haistack/pkg/validate"
)

// FHIRDeidentifierConfig configures the built-in FHIR de-identifier.
type FHIRDeidentifierConfig struct {
	Catalog  *PHICatalog
	Profiles validate.ProfileCatalog // optional StructureDefinition-driven paths
	Rules    *PHIStructureRules
	Engine   fhirpath.Engine // optional; default engine compiles catalog paths
	Redacted string
	// Mode controls StructureDefinition path breadth (default PHIModeStandard).
	Mode PHIMode
	// EvalMode controls which FHIRPath expression strings add segment paths at
	// scrub time (default EvalModeKeywordsOnly). FHIRPath is not evaluated
	// against resource values during scrubbing.
	EvalMode EvalMode
	// UseShared reuses a process-wide de-identifier instance for identical config.
	UseShared bool
}

// FHIRDeidentifier implements Deidentifier using compiled FHIRPath expressions,
// StructureDefinition sensitivity (types, extensions, mustSupport/isSummary),
// PHICatalog paths, passive element detection, and meta.security labels.
type FHIRDeidentifier struct {
	catalog  *PHICatalog
	profiles validate.ProfileCatalog
	rules    PHIStructureRules
	engine   fhirpath.Engine
	index    *fhirPathPHIIndex
	evalMode EvalMode
	Redacted string
}

// NewFHIRDeidentifier returns a de-identifier backed by catalog. When catalog is
// nil, DefaultPHICatalog is used. Use NewFHIRDeidentifierWithConfig for
// StructureDefinition-backed paths.
func NewFHIRDeidentifier(catalog *PHICatalog) (*FHIRDeidentifier, error) {
	return NewFHIRDeidentifierWithConfig(FHIRDeidentifierConfig{Catalog: catalog})
}

// NewFHIRDeidentifierWithConfig constructs a FHIR de-identifier.
func NewFHIRDeidentifierWithConfig(cfg FHIRDeidentifierConfig) (*FHIRDeidentifier, error) {
	if cfg.UseShared {
		return SharedFHIRDeidentifier(cfg)
	}
	return newFHIRDeidentifierWithConfig(cfg)
}

func newFHIRDeidentifierWithConfig(cfg FHIRDeidentifierConfig) (*FHIRDeidentifier, error) {
	catalog := cfg.Catalog
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	rules := DefaultPHIStructureRules()
	if cfg.Rules != nil {
		rules = *cfg.Rules
	}
	mode := cfg.Mode
	if mode == "" {
		mode = PHIModeStandard
	}
	evalMode := cfg.EvalMode
	if evalMode == "" {
		evalMode = EvalModeKeywordsOnly
	}
	engine := cfg.Engine
	if engine == nil {
		var err error
		engine, err = fhirpath.NewEngine(fhirpath.Config{})
		if err != nil {
			return nil, fmt.Errorf("fhirpath engine: %w", err)
		}
	}
	d := &FHIRDeidentifier{
		catalog:  catalog,
		profiles: cfg.Profiles,
		rules:    rules,
		engine:   engine,
		evalMode: evalMode,
		Redacted: cfg.Redacted,
		index:    newFHIRPathPHIIndex(engine, cfg.Profiles, rules, catalog, mode, evalMode),
	}
	if d.Redacted == "" {
		d.Redacted = DefaultRedactedValue
	}
	return d, nil
}

// Deidentify implements Deidentifier.
func (d *FHIRDeidentifier) Deidentify(ctx context.Context, req DeidentifyRequest) (any, []string, error) {
	if d == nil {
		return nil, nil, fmt.Errorf("%w: nil FHIRDeidentifier", ErrMissingDeidentifier)
	}
	placeholder := d.Redacted
	if placeholder == "" {
		placeholder = DefaultRedactedValue
	}

	switch req.ToolName {
	case ToolReadFhirResource:
		if raw, ok := resourceJSONBytes(req.Data); ok {
			return d.deidentifyReadJSON(ctx, req.ResourceType, raw, placeholder, req.ToolName)
		}
		m, err := resourceDataAsMap(req.Data)
		if err != nil {
			return nil, nil, err
		}
		rt := resourceTypeFromMap(m, req.ResourceType)
		redactions, err := d.scrubResource(ctx, rt, m, placeholder, req.ToolName)
		if err != nil {
			return nil, nil, err
		}
		return m, redactions, nil

	case ToolSearchFhirResources:
		root, err := resourceDataAsMap(req.Data)
		if err != nil {
			return nil, nil, err
		}
		var redactions []string
		if resources, ok := root["resources"].([]any); ok {
			for _, item := range resources {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				rt := resourceTypeFromMap(m, req.ResourceType)
				r, err := d.scrubResource(ctx, rt, m, placeholder, req.ToolName)
				if err != nil {
					return nil, nil, err
				}
				redactions = append(redactions, r...)
			}
		}
		if included, ok := root["included"].([]any); ok {
			for _, item := range included {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				rt := resourceTypeFromMap(m, "")
				r, err := d.scrubResource(ctx, rt, m, placeholder, req.ToolName)
				if err != nil {
					return nil, nil, err
				}
				redactions = append(redactions, r...)
			}
		}
		return root, redactions, nil

	case ToolRunView:
		root, ok := req.Data.(map[string]any)
		if !ok {
			return req.Data, nil, nil
		}
		redactions := scrubViewRows(ctx, d, root, placeholder)
		return root, redactions, nil

	default:
		return req.Data, nil, nil
	}
}

func (d *FHIRDeidentifier) deidentifyReadJSON(ctx context.Context, fallbackType string, data []byte, placeholder string, toolName string) (any, []string, error) {
	catalog := d.catalog
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	resolve := d.jsonScrubResolve(ctx, toolName)
	scrubbed, redactions, err := scrubJSONDocument(data, catalog, placeholder, fallbackType, resolve)
	if err != nil {
		return nil, nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(scrubbed, &m); err != nil {
		return nil, nil, fmt.Errorf("deidentify: invalid JSON after scrub: %w", err)
	}
	return m, redactions, nil
}

func (d *FHIRDeidentifier) scrubResource(ctx context.Context, resourceType string, m map[string]any, placeholder string, toolName string) ([]string, error) {
	if m == nil {
		return nil, nil
	}
	catalog := d.catalog
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	data, err := marshalJSONPooled(m)
	if err != nil {
		return nil, err
	}
	resolve := d.jsonScrubResolve(ctx, toolName)
	scrubbed, redactions, err := scrubJSONDocument(data, catalog, placeholder, resourceType, resolve)
	if err != nil {
		return nil, err
	}
	if err := mergeJSONIntoMap(m, scrubbed); err != nil {
		return nil, err
	}
	return redactions, nil
}

func resourceTypeFromMap(m map[string]any, fallback string) string {
	if rt, ok := m["resourceType"].(string); ok && rt != "" {
		return rt
	}
	return fallback
}

func scrubViewRows(ctx context.Context, d *FHIRDeidentifier, viewData map[string]any, placeholder string) []string {
	rows, ok := viewData["rows"].([]any)
	if !ok {
		return nil
	}
	catalog := d.catalog
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	var redactions []string
	for _, row := range rows {
		switch r := row.(type) {
		case map[string]any:
			redactions = append(redactions, scrubViewRowMap(ctx, d, r, placeholder)...)
		case []any:
			cols := columnNamesFromViewData(viewData)
			for i, cell := range r {
				col := ""
				if i < len(cols) {
					col = cols[i]
				}
				if col == "" || !catalog.ViewColumnIsPHI(col) {
					continue
				}
				if cell == nil {
					continue
				}
				r[i] = placeholder
				redactions = append(redactions, "view:"+col)
			}
		}
	}
	return uniqueStrings(redactions)
}

func scrubViewRowMap(ctx context.Context, d *FHIRDeidentifier, row map[string]any, placeholder string) []string {
	catalog := d.catalog
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	var redactions []string
	for col, val := range row {
		if val == nil {
			continue
		}
		if child, ok := val.(map[string]any); ok {
			if _, hasRT := child["resourceType"]; hasRT {
				rt, _ := child["resourceType"].(string)
				childRedactions, err := d.scrubResource(ctx, rt, child, placeholder, ToolRunView)
				if err == nil {
					redactions = append(redactions, childRedactions...)
				}
				row[col] = child
				continue
			}
		}
		if !catalog.ViewColumnIsPHI(col) {
			continue
		}
		row[col] = placeholder
		redactions = append(redactions, "view:"+col)
	}
	return redactions
}

func columnNamesFromViewData(viewData map[string]any) []string {
	cols, ok := viewData["columns"].([]any)
	if !ok {
		return nil
	}
	names := make([]string, len(cols))
	for i, col := range cols {
		c, ok := col.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := c["name"].(string); ok {
			names[i] = name
			continue
		}
		if name, ok := c["Name"].(string); ok {
			names[i] = name
		}
	}
	return names
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
