package validate

import (
	"fmt"
	"strings"
)

func validateProfileSlicing(obj map[string]interface{}, sd *StructureDefinition, issues *[]ValidationIssue) {
	sliceParents := make(map[string]*ElementSlicing)
	for i := range sd.Elements {
		el := &sd.Elements[i]
		if el.Slicing != nil {
			sliceParents[el.Path] = el.Slicing
		}
	}

	for _, el := range sd.Elements {
		if el.Path == "" || el.Path == sd.Type {
			continue
		}
		if strings.Contains(el.Path, "[x]") {
			continue
		}
		parent, _ := splitElementPath(el.Path)
		if parent != sd.Type && parent != "" {
			if countPath(obj, parent) == 0 {
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
		count := countPath(obj, el.Path)
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
	if max, ok := parseMax(el.Max); ok && count > max {
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
	if len(sliceEl.Pattern) > 0 {
		return patternMatches(item, sliceEl.Pattern)
	}
	if slicing == nil || len(slicing.Discriminators) == 0 {
		return false
	}
	for _, disc := range slicing.Discriminators {
		switch disc.Type {
		case "value":
			got, ok := discriminatorValue(item, disc.Path)
			if !ok {
				return false
			}
			if want, ok := sliceEl.Pattern[disc.Path]; ok {
				if !reflectEqual(got, want) {
					return false
				}
				continue
			}
			return false
		default:
			return false
		}
	}
	return false
}

func reflectEqual(a, b interface{}) bool {
	return normalizePatternValue(a) == normalizePatternValue(b)
}
