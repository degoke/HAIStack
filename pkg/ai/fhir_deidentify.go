package ai

import (
	"context"
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
	Redacted string
}

// NewFHIRDeidentifier returns a de-identifier backed by catalog. When catalog is
// nil, DefaultPHICatalog is used. Use NewFHIRDeidentifierWithConfig for
// StructureDefinition-backed paths.
func NewFHIRDeidentifier(catalog *PHICatalog) *FHIRDeidentifier {
	return NewFHIRDeidentifierWithConfig(FHIRDeidentifierConfig{Catalog: catalog})
}

// NewFHIRDeidentifierWithConfig constructs a FHIR de-identifier.
func NewFHIRDeidentifierWithConfig(cfg FHIRDeidentifierConfig) *FHIRDeidentifier {
	catalog := cfg.Catalog
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	rules := DefaultPHIStructureRules()
	if cfg.Rules != nil {
		rules = *cfg.Rules
	}
	engine := cfg.Engine
	if engine == nil {
		engine, _ = fhirpath.NewEngine(fhirpath.Config{})
	}
	d := &FHIRDeidentifier{
		catalog:  catalog,
		profiles: cfg.Profiles,
		rules:    rules,
		engine:   engine,
		Redacted: cfg.Redacted,
		index:    newFHIRPathPHIIndex(engine, cfg.Profiles, rules, catalog),
	}
	if d.Redacted == "" {
		d.Redacted = DefaultRedactedValue
	}
	return d
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
		m, ok := req.Data.(map[string]any)
		if !ok {
			return req.Data, nil, nil
		}
		rt := resourceTypeFromMap(m, req.ResourceType)
		redactions, err := d.scrubResource(ctx, rt, m, placeholder)
		if err != nil {
			return nil, nil, err
		}
		return m, redactions, nil

	case ToolSearchFhirResources:
		root, ok := req.Data.(map[string]any)
		if !ok {
			return req.Data, nil, nil
		}
		var redactions []string
		if resources, ok := root["resources"].([]any); ok {
			for _, item := range resources {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				rt := resourceTypeFromMap(m, req.ResourceType)
				r, err := d.scrubResource(ctx, rt, m, placeholder)
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
				r, err := d.scrubResource(ctx, rt, m, placeholder)
				if err != nil {
					return nil, nil, err
				}
				redactions = append(redactions, r...)
			}
		}
		return root, uniqueStrings(redactions), nil

	case ToolRunView:
		root, ok := req.Data.(map[string]any)
		if !ok {
			return req.Data, nil, nil
		}
		redactions := scrubViewRows(root, d.catalog, placeholder)
		return root, redactions, nil

	default:
		return req.Data, nil, nil
	}
}

func (d *FHIRDeidentifier) scrubResource(ctx context.Context, resourceType string, m map[string]any, placeholder string) ([]string, error) {
	if m == nil {
		return nil, nil
	}
	bundle, err := d.index.bundleFor(ctx, resourceType, m)
	if err != nil {
		return nil, err
	}
	fhirRedactions := redactFHIRPaths(resourceType, m, bundle.segment, placeholder)
	evalRedactions, err := redactCompiledFHIRPaths(ctx, d.engine, resourceType, m, bundle.compiled, bundle.labels, placeholder)
	if err != nil {
		return nil, err
	}
	walkRedactions := deepScrubResource(resourceType, m, d.catalog, placeholder)
	return uniqueStrings(append(append(fhirRedactions, evalRedactions...), walkRedactions...)), nil
}

func resourceTypeFromMap(m map[string]any, fallback string) string {
	if rt, ok := m["resourceType"].(string); ok && rt != "" {
		return rt
	}
	return fallback
}

func scrubViewRows(viewData map[string]any, catalog *PHICatalog, placeholder string) []string {
	rows, ok := viewData["rows"].([]any)
	if !ok {
		return nil
	}
	var redactions []string
	for _, row := range rows {
		switch r := row.(type) {
		case map[string]any:
			redactions = append(redactions, scrubViewRowMap(r, catalog, placeholder)...)
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

func scrubViewRowMap(row map[string]any, catalog *PHICatalog, placeholder string) []string {
	var redactions []string
	for col, val := range row {
		if val == nil {
			continue
		}
		if child, ok := val.(map[string]any); ok {
			rt, _ := child["resourceType"].(string)
			redactions = append(redactions, deepScrubResource(rt, child, catalog, placeholder)...)
			row[col] = child
			continue
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
