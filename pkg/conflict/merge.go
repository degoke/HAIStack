package conflict

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// Operation is one FHIR Patch (JSON Patch) operation.
type Operation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

func rebase(local LocalEvent, base, current *types.ResourceEnvelope, registry *PolicyRegistry) ([]byte, []byte, error) {
	var currentMap map[string]any
	if err := json.Unmarshal(current.JSON, &currentMap); err != nil {
		return nil, nil, fmt.Errorf("unmarshal current: %w", err)
	}

	localChanges, err := diffJSON(base.JSON, local.ResourceAfter.JSON)
	if err != nil {
		return nil, nil, err
	}
	remoteChanges, err := diffJSON(base.JSON, current.JSON)
	if err != nil {
		return nil, nil, err
	}
	overlaps := computeOverlaps(localChanges, remoteChanges, local.ResourceType)

	merged := deepCopyMap(currentMap)
	var ops []Operation

	for _, c := range localChanges {
		path := dottedPath(local.ResourceType, c.Path)
		overlapping := pathOverlapsAny(path, overlaps)
		rule := registry.Match(local.ResourceType, path)

		switch c.Kind {
		case ChangeKindScalarReplace:
			if overlapping && (rule == nil || rule.Semantics != RuleSemanticsAppendOnly) {
				return nil, nil, fmt.Errorf("overlapping scalar change at %s", path)
			}
			if err := setValueAtPath(merged, c.Path, c.Value); err != nil {
				return nil, nil, err
			}
			ops = append(ops, Operation{Op: "replace", Path: jsonPointer(c.Path), Value: c.Value})

		case ChangeKindArrayAppend:
			if overlapping && (rule == nil || rule.Semantics != RuleSemanticsAppendOnly) {
				return nil, nil, fmt.Errorf("overlapping non-append change at %s", path)
			}
			parent, key, err := parentAndKey(merged, c.Path)
			if err != nil {
				return nil, nil, err
			}
			arr, _ := parent[key].([]any)
			start := len(arr)
			for _, it := range c.Appended {
				if !sliceContains(arr, it) {
					arr = append(arr, it)
				}
			}
			parent[key] = arr
			added := arr[start:]
			ptr := jsonPointer(c.Path)
			for _, it := range added {
				ops = append(ops, Operation{Op: "add", Path: ptr + "/-", Value: it})
			}

		default:
			return nil, nil, fmt.Errorf("unsupported change kind %s at %s", c.Kind, path)
		}
	}

	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal merged: %w", err)
	}
	patchJSON, err := json.Marshal(ops)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal patch: %w", err)
	}
	return mergedJSON, patchJSON, nil
}

func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func setValueAtPath(root map[string]any, path []string, value any) error {
	if len(path) == 0 {
		return fmt.Errorf("empty path")
	}
	current := any(root)
	for i, seg := range path[:len(path)-1] {
		switch v := current.(type) {
		case map[string]any:
			next, ok := v[seg]
			if !ok {
				newMap := make(map[string]any)
				v[seg] = newMap
				next = newMap
			}
			current = next
		case []any:
			idx, err := parseIndex(seg)
			if err != nil {
				return fmt.Errorf("path %s: %w", strings.Join(path[:i+1], "."), err)
			}
			if idx < 0 || idx >= len(v) {
				return fmt.Errorf("path index out of range: %s", strings.Join(path[:i+1], "."))
			}
			current = v[idx]
		default:
			return fmt.Errorf("cannot traverse path %s", strings.Join(path[:i+1], "."))
		}
	}
	last := path[len(path)-1]
	switch v := current.(type) {
	case map[string]any:
		v[last] = value
	case []any:
		idx, err := parseIndex(last)
		if err != nil {
			return err
		}
		if idx < 0 || idx >= len(v) {
			return fmt.Errorf("path index out of range: %s", strings.Join(path, "."))
		}
		v[idx] = value
	default:
		return fmt.Errorf("cannot set value at %s", strings.Join(path, "."))
	}
	return nil
}

func parentAndKey(root map[string]any, path []string) (map[string]any, string, error) {
	if len(path) == 0 {
		return nil, "", fmt.Errorf("empty path")
	}
	current := any(root)
	for i, seg := range path[:len(path)-1] {
		switch v := current.(type) {
		case map[string]any:
			next, ok := v[seg]
			if !ok {
				newMap := make(map[string]any)
				v[seg] = newMap
				next = newMap
			}
			current = next
		case []any:
			idx, err := parseIndex(seg)
			if err != nil {
				return nil, "", fmt.Errorf("path %s: %w", strings.Join(path[:i+1], "."), err)
			}
			if idx < 0 || idx >= len(v) {
				return nil, "", fmt.Errorf("path index out of range: %s", strings.Join(path[:i+1], "."))
			}
			current = v[idx]
		default:
			return nil, "", fmt.Errorf("cannot traverse path %s", strings.Join(path[:i+1], "."))
		}
	}
	last := path[len(path)-1]
	if parent, ok := current.(map[string]any); ok {
		return parent, last, nil
	}
	return nil, "", fmt.Errorf("parent is not an object at %s", strings.Join(path, "."))
}

func pathOverlapsAny(path string, overlaps []string) bool {
	for _, ov := range overlaps {
		if pathsOverlap(path, ov) {
			return true
		}
	}
	return false
}

func sliceContains(s []any, item any) bool {
	for _, v := range s {
		if reflect.DeepEqual(v, item) {
			return true
		}
	}
	return false
}

func parseIndex(seg string) (int, error) {
	idx := 0
	if _, err := fmt.Sscanf(seg, "%d", &idx); err != nil {
		return 0, fmt.Errorf("expected array index, got %q", seg)
	}
	return idx, nil
}

func jsonPointer(segments []string) string {
	out := ""
	for _, seg := range segments {
		seg = strings.ReplaceAll(seg, "~", "~0")
		seg = strings.ReplaceAll(seg, "/", "~1")
		out += "/" + seg
	}
	return out
}
