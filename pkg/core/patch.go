package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

type jsonPatchOp struct {
	Op       string
	Path     string
	PathSet  bool
	Value    any
	ValueSet bool
	From     string
	FromSet  bool
}

func (o *jsonPatchOp) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if opRaw, ok := raw["op"]; ok {
		if err := json.Unmarshal(opRaw, &o.Op); err != nil {
			return fmt.Errorf("op must be a string: %w", err)
		}
	}
	if pathRaw, ok := raw["path"]; ok {
		o.PathSet = true
		if err := json.Unmarshal(pathRaw, &o.Path); err != nil {
			return fmt.Errorf("path must be a string: %w", err)
		}
	}
	if fromRaw, ok := raw["from"]; ok {
		o.FromSet = true
		if err := json.Unmarshal(fromRaw, &o.From); err != nil {
			return fmt.Errorf("from must be a string: %w", err)
		}
	}
	if valueRaw, ok := raw["value"]; ok {
		o.ValueSet = true
		if err := json.Unmarshal(valueRaw, &o.Value); err != nil {
			return fmt.Errorf("value is invalid: %w", err)
		}
	}
	return nil
}

// Patch applies a JSON Patch (RFC 6902) to an existing resource.
func (s *ResourceService) Patch(ctx context.Context, resourceType, id string, patchJSON []byte) (*types.ResourceEnvelope, error) {
	if resourceType == "" || id == "" {
		return nil, invalidErr("resourceType and id are required", nil)
	}
	if len(patchJSON) == 0 {
		return nil, invalidErr("patch body is required", nil)
	}

	current, err := s.Read(ctx, resourceType, id)
	if err != nil {
		return nil, err
	}

	patchedJSON, err := applyJSONPatch(current.JSON, patchJSON)
	if err != nil {
		return nil, invalidErr("apply JSON Patch", err)
	}
	if err := validatePatchedIdentity(patchedJSON, resourceType, id); err != nil {
		return nil, err
	}

	envelope := &types.ResourceEnvelope{
		ResourceType: resourceType,
		ID:           id,
		JSON:         patchedJSON,
	}
	return s.Update(ctx, envelope)
}

func validatePatchedIdentity(data []byte, resourceType, id string) error {
	actualType, err := types.GetResourceType(data)
	if err != nil {
		return invalidErr("patched resourceType is invalid", err, "Resource.resourceType")
	}
	if actualType != resourceType {
		return invalidErr(
			fmt.Sprintf("patched resourceType %q does not match %q", actualType, resourceType),
			nil,
			"Resource.resourceType",
		)
	}
	actualID, err := types.GetID(data)
	if err != nil {
		return invalidErr("patched id is invalid", err, "Resource.id")
	}
	if actualID != id {
		return invalidErr(
			fmt.Sprintf("patched id %q does not match %q", actualID, id),
			nil,
			"Resource.id",
		)
	}
	return nil
}

func applyJSONPatch(doc []byte, patchJSON []byte) ([]byte, error) {
	var root any
	if err := json.Unmarshal(doc, &root); err != nil {
		return nil, fmt.Errorf("unmarshal document: %w", err)
	}

	var ops []jsonPatchOp
	if err := json.Unmarshal(patchJSON, &ops); err != nil {
		return nil, fmt.Errorf("unmarshal patch: %w", err)
	}
	for i, op := range ops {
		var err error
		root, err = applyJSONPatchOp(root, op)
		if err != nil {
			return nil, fmt.Errorf("patch operation %d (%s %s): %w", i, op.Op, op.Path, err)
		}
	}

	out, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("marshal patched document: %w", err)
	}
	return out, nil
}

func applyJSONPatchOp(doc any, op jsonPatchOp) (any, error) {
	action := strings.ToLower(strings.TrimSpace(op.Op))
	if action == "" {
		return nil, fmt.Errorf("op is required")
	}
	if !op.PathSet {
		return nil, fmt.Errorf("path is required")
	}
	if _, err := splitJSONPointer(op.Path); err != nil {
		return nil, err
	}
	switch action {
	case "add":
		if !op.ValueSet {
			return nil, fmt.Errorf("add requires value")
		}
		return patchAdd(doc, op.Path, op.Value)
	case "remove":
		return patchRemove(doc, op.Path)
	case "replace":
		if !op.ValueSet {
			return nil, fmt.Errorf("replace requires value")
		}
		return patchReplace(doc, op.Path, op.Value)
	case "move":
		if !op.FromSet {
			return nil, fmt.Errorf("move requires from")
		}
		if _, err := splitJSONPointer(op.From); err != nil {
			return nil, err
		}
		value, err := patchGet(doc, op.From)
		if err != nil {
			return nil, err
		}
		value, err = cloneJSONValue(value)
		if err != nil {
			return nil, err
		}
		doc, err = patchRemove(doc, op.From)
		if err != nil {
			return nil, err
		}
		return patchAdd(doc, op.Path, value)
	case "copy":
		if !op.FromSet {
			return nil, fmt.Errorf("copy requires from")
		}
		if _, err := splitJSONPointer(op.From); err != nil {
			return nil, err
		}
		value, err := patchGet(doc, op.From)
		if err != nil {
			return nil, err
		}
		value, err = cloneJSONValue(value)
		if err != nil {
			return nil, err
		}
		return patchAdd(doc, op.Path, value)
	case "test":
		if !op.ValueSet {
			return nil, fmt.Errorf("test requires value")
		}
		current, err := patchGet(doc, op.Path)
		if err != nil {
			return nil, err
		}
		if !jsonEqual(current, op.Value) {
			return nil, fmt.Errorf("test failed at %s", op.Path)
		}
		return doc, nil
	default:
		return nil, fmt.Errorf("unsupported patch op %q", op.Op)
	}
}

func patchAdd(doc any, path string, value any) (any, error) {
	segments, err := splitJSONPointer(path)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return value, nil
	}
	parent, last, err := patchParent(doc, segments)
	if err != nil {
		return nil, err
	}
	switch node := parent.(type) {
	case map[string]any:
		node[last] = value
		return doc, nil
	case []any:
		if last == "-" {
			node = append(node, value)
		} else {
			idx, err := parseArrayIndex(last, len(node), true)
			if err != nil {
				return nil, err
			}
			node = append(node, nil)
			copy(node[idx+1:], node[idx:])
			node[idx] = value
		}
		return setPatchNode(doc, segments[:len(segments)-1], node)
	default:
		return nil, fmt.Errorf("path parent is not an object or array")
	}
}

func patchReplace(doc any, path string, value any) (any, error) {
	if _, err := patchGet(doc, path); err != nil {
		return nil, err
	}
	segments, err := splitJSONPointer(path)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return value, nil
	}
	parent, last, err := patchParent(doc, segments)
	if err != nil {
		return nil, err
	}
	switch node := parent.(type) {
	case map[string]any:
		node[last] = value
		return doc, nil
	case []any:
		idx, err := parseArrayIndex(last, len(node), false)
		if err != nil {
			return nil, err
		}
		node[idx] = value
		return doc, nil
	default:
		return nil, fmt.Errorf("path parent is not an object or array")
	}
}

func patchRemove(doc any, path string) (any, error) {
	segments, err := splitJSONPointer(path)
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("cannot remove document root")
	}
	parent, last, err := patchParent(doc, segments)
	if err != nil {
		return nil, err
	}
	switch node := parent.(type) {
	case map[string]any:
		if _, ok := node[last]; !ok {
			return nil, fmt.Errorf("path not found: %s", path)
		}
		delete(node, last)
		return doc, nil
	case []any:
		idx, err := parseArrayIndex(last, len(node), false)
		if err != nil {
			return nil, err
		}
		node = append(node[:idx], node[idx+1:]...)
		return setPatchNode(doc, segments[:len(segments)-1], node)
	default:
		return nil, fmt.Errorf("path parent is not an object or array")
	}
}

func patchGet(doc any, path string) (any, error) {
	segments, err := splitJSONPointer(path)
	if err != nil {
		return nil, err
	}
	current := doc
	for _, seg := range segments {
		switch node := current.(type) {
		case map[string]any:
			child, ok := node[seg]
			if !ok {
				return nil, fmt.Errorf("path not found: %s", path)
			}
			current = child
		case []any:
			idx, err := parseArrayIndex(seg, len(node), false)
			if err != nil {
				return nil, err
			}
			current = node[idx]
		default:
			return nil, fmt.Errorf("path not found: %s", path)
		}
	}
	return current, nil
}

func splitJSONPointer(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("JSON Pointer must be empty or begin with /")
	}
	raw := strings.Split(path[1:], "/")
	out := make([]string, len(raw))
	for i, part := range raw {
		var b strings.Builder
		for j := 0; j < len(part); j++ {
			if part[j] != '~' {
				b.WriteByte(part[j])
				continue
			}
			if j+1 >= len(part) || (part[j+1] != '0' && part[j+1] != '1') {
				return nil, fmt.Errorf("invalid JSON Pointer escape in %q", part)
			}
			if part[j+1] == '0' {
				b.WriteByte('~')
			} else {
				b.WriteByte('/')
			}
			j++
		}
		out[i] = b.String()
	}
	return out, nil
}

func patchParent(doc any, segments []string) (any, string, error) {
	if len(segments) == 0 {
		return nil, "", fmt.Errorf("path is empty")
	}
	parent := doc
	for _, seg := range segments[:len(segments)-1] {
		switch node := parent.(type) {
		case map[string]any:
			child, ok := node[seg]
			if !ok {
				return nil, "", fmt.Errorf("path not found")
			}
			parent = child
		case []any:
			idx, err := parseArrayIndex(seg, len(node), false)
			if err != nil {
				return nil, "", err
			}
			parent = node[idx]
		default:
			return nil, "", fmt.Errorf("path parent is not an object or array")
		}
	}
	return parent, segments[len(segments)-1], nil
}

func setPatchNode(doc any, path []string, value any) (any, error) {
	if len(path) == 0 {
		return value, nil
	}
	parent, last, err := patchParent(doc, path)
	if err != nil {
		return nil, err
	}
	switch node := parent.(type) {
	case map[string]any:
		node[last] = value
		return doc, nil
	case []any:
		idx, err := parseArrayIndex(last, len(node), false)
		if err != nil {
			return nil, err
		}
		node[idx] = value
		return doc, nil
	default:
		return nil, fmt.Errorf("path parent is not an object or array")
	}
}

func parseArrayIndex(seg string, length int, allowEnd bool) (int, error) {
	if seg == "-" {
		if allowEnd {
			return length, nil
		}
		return 0, fmt.Errorf("- is not a readable array index")
	}
	if seg == "" || (len(seg) > 1 && seg[0] == '0') {
		return 0, fmt.Errorf("invalid array index %q", seg)
	}
	for _, r := range seg {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid array index %q", seg)
		}
	}
	idx, err := strconv.Atoi(seg)
	if err != nil || idx < 0 || idx > length || (idx == length && !allowEnd) {
		return 0, fmt.Errorf("invalid array index %q", seg)
	}
	return idx, nil
}

func cloneJSONValue(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var clone any
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, err
	}
	return clone, nil
}

func jsonEqual(a, b any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ab) == string(bb)
}
