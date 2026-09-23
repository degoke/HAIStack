package ai

import (
	"context"
	"fmt"
	"strings"
)

// DefaultRedactedValue is the placeholder written when a PHI field is removed.
const DefaultRedactedValue = "[redacted]"

// FHIRDeidentifier implements Deidentifier using PHICatalog deep FHIR paths,
// passive element detection, meta.security confidentiality labels, and view
// column rules. It scrubs resource maps (read/search), contained resources, and
// view rows before tool output is formatted for a model.
type FHIRDeidentifier struct {
	Catalog  *PHICatalog
	Redacted string
}

// NewFHIRDeidentifier returns a de-identifier backed by catalog. When catalog
// is nil, DefaultPHICatalog is used.
func NewFHIRDeidentifier(catalog *PHICatalog) *FHIRDeidentifier {
	return &FHIRDeidentifier{
		Catalog:  catalog,
		Redacted: DefaultRedactedValue,
	}
}

// Deidentify implements Deidentifier.
func (d *FHIRDeidentifier) Deidentify(_ context.Context, req DeidentifyRequest) (any, []string, error) {
	if d == nil {
		return nil, nil, fmt.Errorf("%w: nil FHIRDeidentifier", ErrMissingDeidentifier)
	}
	catalog := d.Catalog
	if catalog == nil {
		catalog = DefaultPHICatalog()
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
		redactions := scrubResourceMap(rt, m, catalog, placeholder)
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
				redactions = append(redactions, scrubResourceMap(rt, m, catalog, placeholder)...)
			}
		}
		if included, ok := root["included"].([]any); ok {
			for _, item := range included {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				rt := resourceTypeFromMap(m, "")
				redactions = append(redactions, scrubResourceMap(rt, m, catalog, placeholder)...)
			}
		}
		return root, uniqueStrings(redactions), nil

	case ToolRunView:
		root, ok := req.Data.(map[string]any)
		if !ok {
			return req.Data, nil, nil
		}
		redactions := scrubViewRows(root, catalog, placeholder)
		return root, redactions, nil

	default:
		return req.Data, nil, nil
	}
}

func resourceTypeFromMap(m map[string]any, fallback string) string {
	if rt, ok := m["resourceType"].(string); ok && rt != "" {
		return rt
	}
	return fallback
}

func scrubResourceMap(resourceType string, m map[string]any, catalog *PHICatalog, placeholder string) []string {
	return deepScrubResource(resourceType, m, catalog, placeholder)
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
