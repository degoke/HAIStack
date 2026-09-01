package validate

import (
	"reflect"
	"strings"
)

func findElement(sd *StructureDefinition, path string) *ElementDefinition {
	for i := range sd.Elements {
		if sd.Elements[i].Path == path {
			return &sd.Elements[i]
		}
	}
	return nil
}

func valuesAtPath(obj map[string]interface{}, path string) []interface{} {
	segments := strings.Split(path, ".")
	if len(segments) < 2 {
		return nil
	}
	return collectValues(obj, segments[1:])
}

func collectValues(node interface{}, segments []string) []interface{} {
	if node == nil || len(segments) == 0 {
		if node == nil {
			return nil
		}
		return []interface{}{node}
	}
	switch current := node.(type) {
	case []interface{}:
		var out []interface{}
		for _, item := range current {
			out = append(out, collectValues(item, segments)...)
		}
		return out
	case map[string]interface{}:
		next, ok := current[segments[0]]
		if !ok || next == nil {
			return nil
		}
		if len(segments) == 1 {
			if arr, ok := next.([]interface{}); ok {
				return arr
			}
			return []interface{}{next}
		}
		return collectValues(next, segments[1:])
	default:
		return nil
	}
}

func patternMatches(item map[string]interface{}, pattern map[string]interface{}) bool {
	if item == nil || len(pattern) == 0 {
		return false
	}
	for key, want := range pattern {
		got, ok := item[key]
		if !ok {
			return false
		}
		if !reflect.DeepEqual(normalizePatternValue(got), normalizePatternValue(want)) {
			return false
		}
	}
	return true
}

func normalizePatternValue(v interface{}) interface{} {
	switch x := v.(type) {
	case float64:
		if x == float64(int64(x)) {
			return int64(x)
		}
		return x
	default:
		return v
	}
}

func discriminatorValue(item map[string]interface{}, path string) (interface{}, bool) {
	if item == nil || path == "" {
		return nil, false
	}
	segments := strings.Split(path, ".")
	var walk interface{} = item
	for _, seg := range segments {
		m, ok := walk.(map[string]interface{})
		if !ok {
			return nil, false
		}
		next, ok := m[seg]
		if !ok {
			return nil, false
		}
		walk = next
	}
	return walk, true
}
