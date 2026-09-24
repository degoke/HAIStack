package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func applyPatchDocument(doc, patchJSON []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(patchJSON)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("patch body is required")
	}
	if trimmed[0] == '{' {
		return applyFHIRPatch(doc, patchJSON)
	}
	return applyJSONPatch(doc, patchJSON)
}

func applyFHIRPatch(doc, patchJSON []byte) ([]byte, error) {
	var params map[string]any
	if err := json.Unmarshal(patchJSON, &params); err != nil {
		return nil, fmt.Errorf("unmarshal FHIR Patch: %w", err)
	}
	if resourceType, _ := params["resourceType"].(string); resourceType != "Parameters" {
		return nil, fmt.Errorf("FHIR Patch body must be a Parameters resource")
	}
	var root any
	if err := json.Unmarshal(doc, &root); err != nil {
		return nil, fmt.Errorf("unmarshal document: %w", err)
	}
	resourceType := jsonMapString(root, "resourceType")
	ops := fhirPatchOperations(params)
	if len(ops) == 0 {
		return nil, fmt.Errorf("FHIR Patch requires at least one operation")
	}
	for i, op := range ops {
		var err error
		root, err = applyFHIRPatchOp(root, resourceType, op)
		if err != nil {
			return nil, fmt.Errorf("patch operation %d (%s): %w", i, op.Type, err)
		}
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("marshal patched document: %w", err)
	}
	return out, nil
}

type fhirPatchOp struct {
	Type  string
	Path  string
	Name  string
	Index *int
	Value any
	From  string
	Dest  string
}

func fhirPatchOperations(params map[string]any) []fhirPatchOp {
	raw, _ := params["parameter"].([]any)
	var ops []fhirPatchOp
	for _, item := range raw {
		pm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := pm["name"].(string); name != "operation" {
			continue
		}
		op := fhirPatchOp{}
		for _, part := range anyMaps(pm["part"]) {
			switch part["name"] {
			case "type":
				op.Type = strings.ToLower(strings.TrimSpace(parameterAnyString(part, "valueCode", "valueString")))
			case "path":
				op.Path = strings.TrimSpace(parameterAnyString(part, "valueString"))
			case "name":
				op.Name = strings.TrimSpace(parameterAnyString(part, "valueString"))
			case "index":
				if n, ok := parameterInt(part); ok {
					op.Index = &n
				}
			case "value":
				op.Value = fhirPatchValue(part)
			case "source":
				op.From = strings.TrimSpace(parameterAnyString(part, "valueString"))
			case "destination":
				op.Dest = strings.TrimSpace(parameterAnyString(part, "valueString"))
			}
		}
		ops = append(ops, op)
	}
	return ops
}

func applyFHIRPatchOp(doc any, resourceType string, op fhirPatchOp) (any, error) {
	switch op.Type {
	case "replace":
		if op.Value == nil {
			return nil, fmt.Errorf("replace requires value")
		}
		return fhirPathReplace(doc, resourceType, op.Path, op.Value)
	case "add":
		if op.Name == "" {
			return nil, fmt.Errorf("add requires name")
		}
		return fhirPathAdd(doc, resourceType, op.Path, op.Name, op.Value)
	case "delete", "remove":
		return fhirPathDelete(doc, resourceType, op.Path)
	case "insert":
		if op.Index == nil {
			return nil, fmt.Errorf("insert requires index")
		}
		return fhirPathInsert(doc, resourceType, op.Path, *op.Index, op.Value)
	case "move":
		if op.From == "" || op.Dest == "" {
			return nil, fmt.Errorf("move requires source and destination")
		}
		return fhirPathMove(doc, resourceType, op.From, op.Dest)
	default:
		return nil, fmt.Errorf("unsupported FHIR Patch type %q", op.Type)
	}
}

func fhirPathReplace(doc any, resourceType, path string, value any) (any, error) {
	segments, err := parseFHIRPatchPath(path, resourceType)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return value, nil
	}
	return setJSONSegments(doc, segments, value, setModeReplace)
}

func fhirPathAdd(doc any, resourceType, path, name string, value any) (any, error) {
	segments, err := parseFHIRPatchPath(path, resourceType)
	if err != nil {
		return nil, err
	}
	parent, err := getJSONSegments(doc, segments, false)
	if err != nil {
		return nil, err
	}
	switch node := parent.(type) {
	case map[string]any:
		existing, ok := node[name]
		if !ok {
			if _, isMap := value.(map[string]any); isMap {
				node[name] = []any{value}
			} else {
				node[name] = value
			}
			return doc, nil
		}
		switch arr := existing.(type) {
		case []any:
			node[name] = append(arr, value)
			return doc, nil
		default:
			// Non-repeating element: replace-if-present.
			node[name] = value
			return doc, nil
		}
	case []any:
		return nil, fmt.Errorf("add name %q targets an array; use insert", name)
	default:
		return nil, fmt.Errorf("add path does not resolve to an object")
	}
}

func fhirPathMove(doc any, resourceType, from, dest string) (any, error) {
	src, err := fhirPathGet(doc, resourceType, from)
	if err != nil {
		return nil, err
	}
	cloned, err := cloneJSONValue(src)
	if err != nil {
		return nil, err
	}
	doc, err = fhirPathDelete(doc, resourceType, from)
	if err != nil {
		return nil, err
	}
	destSegs, err := parseFHIRPatchPath(dest, resourceType)
	if err != nil {
		return nil, err
	}
	if len(destSegs) == 0 {
		return cloned, nil
	}
	parentSegs := destSegs[:len(destSegs)-1]
	last := destSegs[len(destSegs)-1]
	parent, err := getJSONSegments(doc, parentSegs, false)
	if err != nil {
		return nil, err
	}
	switch node := parent.(type) {
	case map[string]any:
		existing, ok := node[last.Name]
		if last.Index != nil {
			arr, isArr := existing.([]any)
			if !isArr {
				return nil, fmt.Errorf("move destination %s is not an array", last.Name)
			}
			idx := *last.Index
			if idx < 0 || idx > len(arr) {
				return nil, fmt.Errorf("move destination index %d is out of range", idx)
			}
			arr = append(arr, nil)
			copy(arr[idx+1:], arr[idx:])
			arr[idx] = cloned
			node[last.Name] = arr
			return doc, nil
		}
		if ok {
			if arr, isArr := existing.([]any); isArr {
				node[last.Name] = append(arr, cloned)
				return doc, nil
			}
		}
		node[last.Name] = cloned
		return doc, nil
	case []any:
		return fhirPathInsert(doc, resourceType, dest, len(node), cloned)
	default:
		return fhirPathReplace(doc, resourceType, dest, cloned)
	}
}

func fhirPathDelete(doc any, resourceType, path string) (any, error) {
	segments, err := parseFHIRPatchPath(path, resourceType)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("cannot delete document root")
	}
	return setJSONSegments(doc, segments, nil, setModeDelete)
}

func fhirPathInsert(doc any, resourceType, path string, index int, value any) (any, error) {
	segments, err := parseFHIRPatchPath(path, resourceType)
	if err != nil {
		return nil, err
	}
	parent, err := getJSONSegments(doc, segments, false)
	if err != nil {
		return nil, err
	}
	arr, ok := parent.([]any)
	if !ok {
		return nil, fmt.Errorf("insert path does not resolve to an array")
	}
	if index < 0 || index > len(arr) {
		return nil, fmt.Errorf("insert index %d is out of range", index)
	}
	arr = append(arr, nil)
	copy(arr[index+1:], arr[index:])
	arr[index] = value
	if len(segments) == 0 {
		return arr, nil
	}
	return setJSONSegments(doc, segments, arr, setModeReplace)
}

func fhirPathGet(doc any, resourceType, path string) (any, error) {
	segments, err := parseFHIRPatchPath(path, resourceType)
	if err != nil {
		return nil, err
	}
	return getJSONSegments(doc, segments, false)
}

type fhirPathSeg struct {
	Name  string
	Index *int
}

func parseFHIRPatchPath(path, resourceType string) ([]fhirPathSeg, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if strings.ContainsAny(path, "() ") {
		return nil, fmt.Errorf("FHIRPath functions are not supported in Patch path %q", path)
	}
	raw := strings.Split(path, ".")
	var segs []fhirPathSeg
	for i, part := range raw {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("invalid path %q", path)
		}
		name := part
		var idx *int
		if br := strings.IndexByte(part, '['); br >= 0 {
			if !strings.HasSuffix(part, "]") {
				return nil, fmt.Errorf("invalid path index in %q", part)
			}
			name = part[:br]
			n, err := strconv.Atoi(part[br+1 : len(part)-1])
			if err != nil {
				return nil, fmt.Errorf("invalid path index in %q", part)
			}
			idx = &n
		}
		if i == 0 && resourceType != "" && name == resourceType {
			if idx != nil {
				return nil, fmt.Errorf("invalid path %q", path)
			}
			continue
		}
		segs = append(segs, fhirPathSeg{Name: name, Index: idx})
	}
	return segs, nil
}

type setMode int

const (
	setModeReplace setMode = iota
	setModeDelete
)

func getJSONSegments(doc any, segments []fhirPathSeg, allowMissing bool) (any, error) {
	current := doc
	for _, seg := range segments {
		switch node := current.(type) {
		case map[string]any:
			child, ok := node[seg.Name]
			if !ok {
				if allowMissing {
					return nil, nil
				}
				return nil, fmt.Errorf("path not found: %s", seg.Name)
			}
			current = child
		default:
			return nil, fmt.Errorf("path not found: %s", seg.Name)
		}
		if seg.Index != nil {
			arr, ok := current.([]any)
			if !ok {
				return nil, fmt.Errorf("path %s is not an array", seg.Name)
			}
			if *seg.Index < 0 || *seg.Index >= len(arr) {
				return nil, fmt.Errorf("index %d out of range for %s", *seg.Index, seg.Name)
			}
			current = arr[*seg.Index]
		}
	}
	return current, nil
}

func setJSONSegments(doc any, segments []fhirPathSeg, value any, mode setMode) (any, error) {
	if len(segments) == 0 {
		if mode == setModeDelete {
			return nil, fmt.Errorf("cannot delete document root")
		}
		return value, nil
	}
	return mutateJSON(doc, segments, value, mode)
}

func mutateJSON(current any, segments []fhirPathSeg, value any, mode setMode) (any, error) {
	seg := segments[0]
	last := len(segments) == 1
	switch node := current.(type) {
	case map[string]any:
		child, ok := node[seg.Name]
		if seg.Index != nil {
			if !ok {
				return nil, fmt.Errorf("path not found: %s", seg.Name)
			}
			arr, isArr := child.([]any)
			if !isArr {
				return nil, fmt.Errorf("path %s is not an array", seg.Name)
			}
			idx := *seg.Index
			if idx < 0 || idx >= len(arr) {
				return nil, fmt.Errorf("index %d out of range for %s", idx, seg.Name)
			}
			if last {
				if mode == setModeDelete {
					node[seg.Name] = append(arr[:idx], arr[idx+1:]...)
					return node, nil
				}
				arr[idx] = value
				node[seg.Name] = arr
				return node, nil
			}
			updated, err := mutateJSON(arr[idx], segments[1:], value, mode)
			if err != nil {
				return nil, err
			}
			arr[idx] = updated
			node[seg.Name] = arr
			return node, nil
		}
		if last {
			if mode == setModeDelete {
				if !ok {
					return nil, fmt.Errorf("path not found: %s", seg.Name)
				}
				delete(node, seg.Name)
				return node, nil
			}
			node[seg.Name] = value
			return node, nil
		}
		if !ok {
			return nil, fmt.Errorf("path not found: %s", seg.Name)
		}
		updated, err := mutateJSON(child, segments[1:], value, mode)
		if err != nil {
			return nil, err
		}
		node[seg.Name] = updated
		return node, nil
	default:
		return nil, fmt.Errorf("path parent is not an object")
	}
}

// ValidateFHIRResourcePath checks that path is a supported FHIR path for resource updates.
func ValidateFHIRResourcePath(resourceType, path string) error {
	_, err := parseFHIRPatchPath(strings.TrimSpace(path), resourceType)
	return err
}

// ApplyPathUpdates applies FHIR-path keyed values to an in-memory resource JSON object.
// Path keys follow the same rules as FHIR Patch paths (see parseFHIRPatchPath).
func ApplyPathUpdates(resourceType string, root map[string]any, patches map[string]any) error {
	if root == nil {
		return fmt.Errorf("resource document is required")
	}
	for path, value := range patches {
		segs, err := parseFHIRPatchPath(path, resourceType)
		if err != nil {
			return fmt.Errorf("patch %q: %w", path, err)
		}
		updated, err := setJSONSegments(root, segs, value, setModeReplace)
		if err != nil {
			return fmt.Errorf("patch %q: %w", path, err)
		}
		if updatedMap, ok := updated.(map[string]any); ok && len(segs) > 0 {
			// setJSONSegments returns the root when mutation is in-place; keep root synced.
			for k, v := range updatedMap {
				root[k] = v
			}
		}
	}
	return nil
}

func fhirPatchValue(part map[string]any) any {
	if res, ok := part["resource"]; ok {
		return res
	}
	for key, val := range part {
		if strings.HasPrefix(key, "value") && key != "value" {
			return val
		}
	}
	if val, ok := part["value"]; ok {
		return val
	}
	return nil
}

func parameterAnyString(p map[string]any, keys ...string) string {
	for _, key := range keys {
		switch v := p[key].(type) {
		case string:
			if v != "" {
				return v
			}
		}
	}
	return ""
}

func parameterInt(p map[string]any) (int, bool) {
	for _, key := range []string{"valueInteger", "valueUnsignedInt", "valuePositiveInt"} {
		switch v := p[key].(type) {
		case float64:
			return int(v), true
		case json.Number:
			n, err := v.Int64()
			if err == nil {
				return int(n), true
			}
		case string:
			n, err := strconv.Atoi(v)
			if err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func anyMaps(raw any) []map[string]any {
	items, _ := raw.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func jsonMapString(doc any, key string) string {
	obj, ok := doc.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := obj[key].(string)
	return s
}
