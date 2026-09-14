package conceptmap

import (
	"context"
	"fmt"
	"strings"
)

// RemoteTranslateClient performs ConceptMap translation against an external terminology server.
type RemoteTranslateClient interface {
	Translate(ctx context.Context, req TranslateRequest) ([]map[string]any, error)
}

// Translator translates source codings using ConceptMap resources.
type Translator struct {
	Resolver Resolver
	Remote   RemoteTranslateClient
}

// TranslateRequest identifies a source coding and ConceptMap to apply.
type TranslateRequest struct {
	MapCanonical string
	Source       map[string]any
	TargetSystem string
}

// Translate returns target codings for a source coding.
func (t Translator) Translate(ctx context.Context, req TranslateRequest) ([]map[string]any, error) {
	if req.MapCanonical == "" {
		return nil, fmt.Errorf("ConceptMap canonical URL is required")
	}
	if t.Resolver != nil {
		codings, err := t.translateLocal(ctx, req)
		if err == nil {
			return codings, nil
		}
		if t.Remote == nil || !IsNotFound(err) {
			return nil, err
		}
	}
	if t.Remote == nil {
		return nil, fmt.Errorf("ConceptMap resolver is unavailable")
	}
	return t.Remote.Translate(ctx, req)
}

func (t Translator) translateLocal(ctx context.Context, req TranslateRequest) ([]map[string]any, error) {
	m, err := t.Resolver.Resolve(ctx, req.MapCanonical)
	if err != nil {
		return nil, err
	}
	sourceSystem, sourceCode, sourceDisplay := codingParts(req.Source)
	if sourceCode == "" {
		return nil, fmt.Errorf("translate source coding is required")
	}
	var matches []map[string]any
	unmappedApplied := false
	for _, group := range m.Group {
		if req.TargetSystem != "" && group.Target != "" && group.Target != req.TargetSystem {
			continue
		}
		if sourceSystem != "" && group.Source != "" && group.Source != sourceSystem {
			continue
		}
		groupMatched := false
		for _, element := range group.Element {
			if element.Code != sourceCode {
				continue
			}
			groupMatched = true
			if element.NoMap {
				return nil, fmt.Errorf("ConceptMap %s marks source code %q as no-map", req.MapCanonical, sourceCode)
			}
			for _, target := range element.Target {
				if !acceptableEquivalence(target.Equivalence, target.Relationship) {
					continue
				}
				matches = append(matches, targetCoding(group, target))
			}
		}
		if !groupMatched && !unmappedApplied {
			unmapped, err := unmappedCoding(group, sourceSystem, sourceCode, sourceDisplay)
			if err != nil {
				return nil, err
			}
			if unmapped != nil {
				matches = append(matches, unmapped)
				unmappedApplied = true
			}
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("ConceptMap %s has no translation for code %q", req.MapCanonical, sourceCode)
	}
	return dedupeCodings(matches), nil
}

func dedupeCodings(matches []map[string]any) []map[string]any {
	seen := map[string]struct{}{}
	out := make([]map[string]any, 0, len(matches))
	for _, coding := range matches {
		key := codingKey(coding)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, coding)
	}
	return out
}

func codingKey(coding map[string]any) string {
	system, _ := coding["system"].(string)
	code, _ := coding["code"].(string)
	display, _ := coding["display"].(string)
	return system + "\x00" + code + "\x00" + display
}

func targetCoding(group Group, target Target) map[string]any {
	coding := map[string]any{"code": target.Code}
	if group.Target != "" {
		coding["system"] = group.Target
	}
	if target.Display != "" {
		coding["display"] = target.Display
	}
	return coding
}

func unmappedCoding(group Group, sourceSystem, sourceCode, sourceDisplay string) (map[string]any, error) {
	if group.Unmapped == nil {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(group.Unmapped.Mode)) {
	case "fixed":
		coding := map[string]any{"code": group.Unmapped.Code}
		if group.Target != "" {
			coding["system"] = group.Target
		}
		if group.Unmapped.Display != "" {
			coding["display"] = group.Unmapped.Display
		}
		return coding, nil
	case "provided":
		coding := map[string]any{"code": sourceCode}
		if sourceSystem != "" {
			coding["system"] = sourceSystem
		} else if group.Source != "" {
			coding["system"] = group.Source
		}
		if sourceDisplay != "" {
			coding["display"] = sourceDisplay
		}
		return coding, nil
	case "use-source-code":
		coding := map[string]any{"code": sourceCode}
		if group.Target != "" {
			coding["system"] = group.Target
		}
		if sourceDisplay != "" {
			coding["display"] = sourceDisplay
		}
		return coding, nil
	case "disabled":
		return nil, fmt.Errorf("ConceptMap group has unmapped mode disabled for code %q", sourceCode)
	default:
		return nil, nil
	}
}

func codingParts(coding map[string]any) (system, code, display string) {
	if coding == nil {
		return "", "", ""
	}
	if v, ok := coding["system"].(string); ok {
		system = v
	}
	if v, ok := coding["code"].(string); ok {
		code = v
	}
	if v, ok := coding["display"].(string); ok {
		display = v
	}
	return system, code, display
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
