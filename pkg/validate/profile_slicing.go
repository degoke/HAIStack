package validate

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

func validateProfileSlicing(ctx context.Context, obj map[string]interface{}, sd *StructureDefinition, issues *[]ValidationIssue) {
	state := newProfileStructureState(sd)
	accumulateProfilePathCounts(obj, sd.Type, state.counts)
	validateProfileSlicingWithCounts(ctx, obj, sd, state, issues)
}

func validateProfileSlicingWithCounts(ctx context.Context, obj map[string]interface{}, sd *StructureDefinition, state *profileStructureState, issues *[]ValidationIssue) {
	sliceParents := state.sliceParents
	for _, el := range sd.Elements {
		if err := ctx.Err(); err != nil {
			return
		}
		if el.Path == "" || el.Path == sd.Type {
			continue
		}
		if strings.Contains(el.Path, "[x]") {
			continue
		}
		parent, _ := splitElementPath(el.Path)
		if parent != sd.Type && parent != "" {
			if state.pathCount(parent) == 0 {
				continue
			}
		}

		if el.SliceName != "" {
			count := countSliceMatches(obj, el, sd, sliceParents)
			reportSliceCardinality(el, count, sd.URL, issues)
			continue
		}
		if _, sliced := sliceParents[el.Path]; sliced {
			continue
		}
		count := state.pathCount(el.Path)
		reportSliceCardinality(el, count, sd.URL, issues)
	}
}

func reportSliceCardinality(el ElementDefinition, count int, profileURL string, issues *[]ValidationIssue) {
	if el.Min > 0 && count < el.Min {
		*issues = append(*issues, issue(
			"required",
			fmt.Sprintf("%s: minimum required = %d, but only found %d (%s)", el.Path, el.Min, count, profileURL),
			[]string{el.Path},
		))
	}
	max, bounded, invalid := parseCardinalityMax(el.Max)
	if invalid {
		*issues = append(*issues, issue(
			"structure",
			fmt.Sprintf("%s: invalid max cardinality %q (%s)", el.Path, el.Max, profileURL),
			[]string{el.Path},
		))
		return
	}
	if bounded && count > max {
		*issues = append(*issues, issue(
			"structure",
			fmt.Sprintf("%s: max allowed = %d, but found %d (%s)", el.Path, max, count, profileURL),
			[]string{el.Path},
		))
	}
}

func countSliceMatches(obj map[string]interface{}, sliceEl ElementDefinition, sd *StructureDefinition, sliceParents map[string]*ElementSlicing) int {
	parentPath, _ := splitElementPath(sliceEl.Path)
	items := valuesAtPath(obj, parentPath)
	if len(items) == 0 {
		return 0
	}
	parentEl := findElement(sd, parentPath)
	var slicing *ElementSlicing
	if parentEl != nil && parentEl.Slicing != nil {
		slicing = parentEl.Slicing
	} else if s, ok := sliceParents[parentPath]; ok {
		slicing = s
	}
	count := 0
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if sliceItemMatches(item, sliceEl, slicing) {
			count++
		}
	}
	return count
}

func sliceItemMatches(item map[string]interface{}, sliceEl ElementDefinition, slicing *ElementSlicing) bool {
	if slicing != nil && len(slicing.Discriminators) > 0 {
		for _, disc := range slicing.Discriminators {
			if !discriminatorMatches(item, disc, sliceEl) {
				return false
			}
		}
		return true
	}
	if len(sliceEl.Pattern) > 0 {
		return patternMatches(item, sliceEl.Pattern)
	}
	return false
}

func discriminatorMatches(item map[string]interface{}, disc SliceDiscriminator, sliceEl ElementDefinition) bool {
	switch strings.ToLower(strings.TrimSpace(disc.Type)) {
	case "value":
		got, ok := discriminatorValue(item, disc.Path)
		if !ok {
			return false
		}
		if want, ok := sliceEl.Pattern[disc.Path]; ok {
			return reflectEqual(got, want)
		}
		return false
	case "exists":
		_, present := discriminatorValue(item, disc.Path)
		if sliceEl.Min > 0 || len(sliceEl.Pattern) > 0 {
			return present
		}
		return !present
	case "pattern":
		got, ok := discriminatorValue(item, disc.Path)
		if !ok {
			return sliceEl.Min == 0 && len(sliceEl.Pattern) == 0
		}
		if want, ok := sliceEl.Pattern[disc.Path]; ok {
			if sub, ok := want.(map[string]interface{}); ok {
				gotMap, ok := got.(map[string]interface{})
				if !ok {
					return false
				}
				return patternMatches(gotMap, sub)
			}
			return reflectEqual(got, want)
		}
		return patternMatches(item, sliceEl.Pattern)
	case "type":
		return itemMatchesTypeDiscriminator(item, disc.Path, sliceEl.Types)
	case "profile":
		return itemMatchesProfileDiscriminator(item, disc.Path, sliceEl.Pattern)
	default:
		return false
	}
}

func itemMatchesTypeDiscriminator(item map[string]interface{}, path string, types []string) bool {
	if len(types) == 0 {
		return false
	}
	if path == "" || path == "value" {
		for _, typ := range types {
			if key := jsonKeyForFHIRTypeChoice(typ); key != "" {
				if _, ok := item[key]; ok {
					return true
				}
			}
		}
		return false
	}
	val, ok := discriminatorValue(item, path)
	if !ok {
		return false
	}
	for _, typ := range types {
		if valueMatchesFHIRType(val, typ) {
			return true
		}
	}
	return false
}

func jsonKeyForFHIRTypeChoice(typeCode string) string {
	typeCode = strings.TrimSpace(typeCode)
	if typeCode == "" {
		return ""
	}
	runes := []rune(typeCode)
	runes[0] = unicode.ToUpper(runes[0])
	return "value" + string(runes)
}

func valueMatchesFHIRType(val interface{}, typeCode string) bool {
	switch strings.ToLower(strings.TrimSpace(typeCode)) {
	case "string", "code", "id", "uri", "url", "uuid", "markdown", "oid", "canonical", "time", "date", "datetime", "instant":
		_, ok := val.(string)
		return ok
	case "boolean":
		_, ok := val.(bool)
		return ok
	case "integer", "positiveint", "unsignedint", "decimal":
		switch val.(type) {
		case float64, int, int64:
			return true
		default:
			return false
		}
	default:
		_, ok := val.(map[string]interface{})
		return ok
	}
}

func itemMatchesProfileDiscriminator(item map[string]interface{}, path string, pattern map[string]interface{}) bool {
	target := item
	if path != "" {
		val, ok := discriminatorValue(item, path)
		if !ok {
			return false
		}
		gotMap, ok := val.(map[string]interface{})
		if !ok {
			return false
		}
		target = gotMap
	}
	if wantRef, ok := pattern["reference"].(string); ok && wantRef != "" {
		ref, _ := target["reference"].(string)
		return ref == wantRef || strings.HasSuffix(ref, "/"+strings.TrimPrefix(wantRef, "/"))
	}
	wantProfiles := profileURLsFromPattern(pattern)
	if len(wantProfiles) == 0 {
		return len(pattern) == 0
	}
	gotProfiles := metaProfiles(target)
	for _, want := range wantProfiles {
		for _, got := range gotProfiles {
			if got == want {
				return true
			}
		}
	}
	return false
}

func profileURLsFromPattern(pattern map[string]interface{}) []string {
	meta, _ := pattern["meta"].(map[string]interface{})
	if meta == nil {
		return nil
	}
	return metaProfiles(map[string]interface{}{"meta": meta})
}

func reflectEqual(a, b interface{}) bool {
	return normalizePatternValue(a) == normalizePatternValue(b)
}

// SliceItemMatchesForTest exposes slice matching for unit tests.
func SliceItemMatchesForTest(item map[string]interface{}, sliceEl ElementDefinition, slicing *ElementSlicing) bool {
	return sliceItemMatches(item, sliceEl, slicing)
}
