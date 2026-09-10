package conceptmap

import (
	"context"
	"fmt"
	"strings"
)

// Translator translates source codings using ConceptMap resources.
type Translator struct {
	Resolver Resolver
}

// TranslateRequest identifies a source coding and ConceptMap to apply.
type TranslateRequest struct {
	MapCanonical string
	Source       map[string]any
	TargetSystem string
}

// Translate returns target codings for a source coding.
func (t Translator) Translate(ctx context.Context, req TranslateRequest) ([]map[string]any, error) {
	if t.Resolver == nil {
		return nil, fmt.Errorf("ConceptMap resolver is unavailable")
	}
	if req.MapCanonical == "" {
		return nil, fmt.Errorf("ConceptMap canonical URL is required")
	}
	m, err := t.Resolver.Resolve(ctx, req.MapCanonical)
	if err != nil {
		return nil, err
	}
	sourceSystem, sourceCode := codingParts(req.Source)
	if sourceCode == "" {
		return nil, fmt.Errorf("translate source coding is required")
	}
	var matches []map[string]any
	for _, group := range m.Group {
		if req.TargetSystem != "" && group.Target != "" && group.Target != req.TargetSystem {
			continue
		}
		if sourceSystem != "" && group.Source != "" && group.Source != sourceSystem {
			continue
		}
		for _, element := range group.Element {
			if element.Code != sourceCode {
				continue
			}
			if element.NoMap {
				return nil, fmt.Errorf("ConceptMap %s marks source code %q as no-map", req.MapCanonical, sourceCode)
			}
			for _, target := range element.Target {
				if !acceptableEquivalence(target.Equivalence, target.Relationship) {
					continue
				}
				coding := map[string]any{"code": target.Code}
				if group.Target != "" {
					coding["system"] = group.Target
				}
				if target.Display != "" {
					coding["display"] = target.Display
				}
				matches = append(matches, coding)
			}
		}
		if len(matches) == 0 && group.Unmapped != nil {
			switch strings.ToLower(group.Unmapped.Mode) {
			case "fixed":
				coding := map[string]any{"code": group.Unmapped.Code}
				if group.Target != "" {
					coding["system"] = group.Target
				}
				if group.Unmapped.Display != "" {
					coding["display"] = group.Unmapped.Display
				}
				matches = append(matches, coding)
			}
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("ConceptMap %s has no translation for code %q", req.MapCanonical, sourceCode)
	}
	return matches, nil
}

func codingParts(coding map[string]any) (system, code string) {
	if coding == nil {
		return "", ""
	}
	if v, ok := coding["system"].(string); ok {
		system = v
	}
	if v, ok := coding["code"].(string); ok {
		code = v
	}
	return system, code
}

func acceptableEquivalence(equivalence, relationship string) bool {
	value := strings.ToLower(strings.TrimSpace(firstNonEmpty(equivalence, relationship)))
	switch value {
	case "", "equivalent", "equal", "relatedto", "related-to", "wider", "narrower", "subsumes", "specializes", "source-is-broader-than-target", "source-is-narrower-than-target":
		return true
	case "disjoint", "unmatched", "not-related-to", "inexact":
		return false
	default:
		return true
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
