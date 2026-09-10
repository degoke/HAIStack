package structuremap

import (
	"context"
	"fmt"
	"strings"
)

func navigateElements(value any, elements []string) []any {
	if len(elements) == 0 {
		return []any{value}
	}
	var out []any
	for _, current := range flattenValue(value) {
		next, ok := navigateElement(current, elements[0])
		if !ok {
			continue
		}
		if len(elements) == 1 {
			out = append(out, flattenValue(next)...)
			continue
		}
		out = append(out, navigateElements(next, elements[1:])...)
	}
	return out
}

func navigateElement(value any, element string) (any, bool) {
	if element == "" {
		return value, true
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	next, ok := object[element]
	return next, ok
}

func flattenValue(value any) []any {
	switch v := value.(type) {
	case nil:
		return nil
	case []any:
		return v
	default:
		return []any{value}
	}
}

func assignElementValue(ctx context.Context, root map[string]any, elements []string, value any, listModes []string, cardinality CardinalityResolver) error {
	if len(elements) == 0 {
		return fmt.Errorf("target element path is empty")
	}
	cur := root
	for i, part := range elements {
		if i == len(elements)-1 {
			if hasListMode(listModes, "share") || hasListMode(listModes, "collate") {
				return appendElementPath(ctx, root, cur, elements, value, cardinality)
			}
			if isRepeatingAtPath(ctx, root, elements[:i+1], cardinality) && !hasListMode(listModes, "single") {
				return assignRepeatingValue(cur, part, value)
			}
			cur[part] = value
			return nil
		}
		if isRepeatingAtPath(ctx, root, elements[:i+1], cardinality) && !hasListMode(listModes, "single") {
			next, err := ensureRepeatingObjectElement(cur, part)
			if err != nil {
				return err
			}
			cur = next
			continue
		}
		next, ok := objectElement(cur[part])
		if !ok {
			next = map[string]any{}
			cur[part] = next
		}
		cur = next
	}
	return nil
}

func objectElement(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case []any:
		if len(typed) == 0 {
			return nil, false
		}
		if object, ok := typed[0].(map[string]any); ok {
			return object, true
		}
	}
	return nil, false
}

func ensureRepeatingObjectElement(cur map[string]any, part string) (map[string]any, error) {
	existing := cur[part]
	switch typed := existing.(type) {
	case nil:
		child := map[string]any{}
		cur[part] = []any{child}
		return child, nil
	case []any:
		if len(typed) == 0 {
			child := map[string]any{}
			cur[part] = []any{child}
			return child, nil
		}
		if child, ok := typed[len(typed)-1].(map[string]any); ok {
			return child, nil
		}
		return nil, fmt.Errorf("expected object in repeating field %q", part)
	case map[string]any:
		cur[part] = []any{typed}
		return typed, nil
	default:
		return nil, fmt.Errorf("unexpected value in repeating field %q", part)
	}
}

func assignRepeatingValue(cur map[string]any, part string, value any) error {
	existing := cur[part]
	switch typed := existing.(type) {
	case nil:
		cur[part] = []any{value}
	case []any:
		cur[part] = append(typed, value)
	default:
		cur[part] = []any{typed, value}
	}
	return nil
}

func appendElementPath(ctx context.Context, root map[string]any, cur map[string]any, elements []string, value any, cardinality CardinalityResolver) error {
	if len(elements) == 0 {
		return fmt.Errorf("target element path is empty")
	}
	for i, part := range elements {
		if i == len(elements)-1 {
			existing := cur[part]
			switch typed := existing.(type) {
			case nil:
				cur[part] = value
			case []any:
				cur[part] = append(typed, value)
			default:
				cur[part] = []any{typed, value}
			}
			return nil
		}
		next, ok := objectElement(cur[part])
		if !ok {
			if isRepeatingAtPath(ctx, root, elements[:i+1], cardinality) {
				next, err := ensureRepeatingObjectElement(cur, part)
				if err != nil {
					return err
				}
				cur = next
				continue
			}
			next = map[string]any{}
			cur[part] = next
		}
		cur = next
	}
	return nil
}

func resourceTypeName(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return ""
	}
	if strings.Contains(typeName, "/") {
		parts := strings.Split(typeName, "/")
		return parts[len(parts)-1]
	}
	return typeName
}

func cloneValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			out[key] = cloneValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}
