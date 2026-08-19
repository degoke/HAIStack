package ai

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func parseReadInput(input map[string]any) (ReadInput, error) {
	rt, err := requireString(input, "resourceType")
	if err != nil {
		return ReadInput{}, err
	}
	id, err := requireString(input, "id")
	if err != nil {
		return ReadInput{}, err
	}
	return ReadInput{ResourceType: rt, ID: id}, nil
}

func parseSearchInput(input map[string]any) (SearchInput, error) {
	rt, err := requireString(input, "resourceType")
	if err != nil {
		return SearchInput{}, err
	}
	params, err := parseStringSliceMap(input["params"])
	if err != nil {
		return SearchInput{}, fmt.Errorf("%w: params: %v", ErrInvalidInput, err)
	}
	count, err := optionalNonNegativeInt(input, "count")
	if err != nil {
		return SearchInput{}, err
	}
	offset, err := optionalNonNegativeInt(input, "offset")
	if err != nil {
		return SearchInput{}, err
	}
	return SearchInput{
		ResourceType: rt,
		Params:       params,
		Count:        count,
		Offset:       offset,
	}, nil
}

func parseViewInput(input map[string]any) (ViewInput, error) {
	name, err := requireString(input, "viewName")
	if err != nil {
		return ViewInput{}, err
	}
	version, err := optionalStringValue(input, "version")
	if err != nil {
		return ViewInput{}, err
	}
	params := map[string]any{}
	if raw, ok := input["parameters"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return ViewInput{}, fmt.Errorf("%w: parameters must be an object", ErrInvalidInput)
		}
		params = m
	}
	limit, err := optionalNonNegativeInt(input, "limit")
	if err != nil {
		return ViewInput{}, err
	}
	offset, err := optionalNonNegativeInt(input, "offset")
	if err != nil {
		return ViewInput{}, err
	}
	return ViewInput{
		ViewName:   name,
		Version:    version,
		Parameters: params,
		Limit:      limit,
		Offset:     offset,
	}, nil
}

func parseWriteInput(input map[string]any) (WriteInput, error) {
	op, err := requireString(input, "operation")
	if err != nil {
		return WriteInput{}, err
	}
	if op != "create" && op != "update" {
		return WriteInput{}, fmt.Errorf("%w: operation must be create or update", ErrInvalidInput)
	}
	rt, err := requireString(input, "resourceType")
	if err != nil {
		return WriteInput{}, err
	}
	id, err := optionalStringValue(input, "id")
	if err != nil {
		return WriteInput{}, err
	}
	if op == "update" && id == "" {
		return WriteInput{}, fmt.Errorf("%w: id is required for update", ErrInvalidInput)
	}
	fields, err := parseFields(input["fields"])
	if err != nil {
		return WriteInput{}, err
	}
	if len(fields) == 0 {
		return WriteInput{}, fmt.Errorf("%w: at least one field is required", ErrInvalidInput)
	}
	return WriteInput{
		Operation:    op,
		ResourceType: rt,
		ID:           id,
		Fields:       fields,
	}, nil
}

func parseFields(raw any) (map[string]any, error) {
	if raw == nil {
		return nil, fmt.Errorf("%w: fields is required", ErrInvalidInput)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: fields must be an object", ErrInvalidInput)
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out, nil
}

func parseStringSliceMap(raw any) (map[string][]string, error) {
	if raw == nil {
		return map[string][]string{}, nil
	}
	switch v := raw.(type) {
	case map[string][]string:
		out := make(map[string][]string, len(v))
		for k, vals := range v {
			out[k] = append([]string(nil), vals...)
		}
		return out, nil
	case map[string]any:
		out := make(map[string][]string, len(v))
		for k, val := range v {
			switch typed := val.(type) {
			case string:
				out[k] = []string{typed}
			case []string:
				out[k] = append([]string(nil), typed...)
			case []any:
				vals := make([]string, 0, len(typed))
				for _, item := range typed {
					s, ok := item.(string)
					if !ok {
						return nil, fmt.Errorf("param %q value must be string", k)
					}
					vals = append(vals, s)
				}
				out[k] = vals
			default:
				return nil, fmt.Errorf("param %q has unsupported type", k)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("params must be an object")
	}
}

func requireString(input map[string]any, key string) (string, error) {
	v, ok := input[key]
	if !ok {
		return "", fmt.Errorf("%w: %s is required", ErrInvalidInput, key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("%w: %s must be a non-empty string", ErrInvalidInput, key)
	}
	return s, nil
}

func optionalStringValue(input map[string]any, key string) (string, error) {
	v, ok := input[key]
	if !ok {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%w: %s must be a string", ErrInvalidInput, key)
	}
	return s, nil
}

func optionalNonNegativeInt(input map[string]any, key string) (int, error) {
	v, ok := input[key]
	if !ok {
		return 0, nil
	}
	var value int
	switch n := v.(type) {
	case int:
		value = n
	case int64:
		value = int(n)
	case float64:
		if n != float64(int(n)) {
			return 0, fmt.Errorf("%w: %s must be an integer", ErrInvalidInput, key)
		}
		value = int(n)
	case string:
		parsed, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("%w: %s must be an integer", ErrInvalidInput, key)
		}
		value = parsed
	default:
		return 0, fmt.Errorf("%w: %s must be an integer", ErrInvalidInput, key)
	}
	if value < 0 {
		return 0, fmt.Errorf("%w: %s must be non-negative", ErrInvalidInput, key)
	}
	return value, nil
}

// applyFields merges approved top-level fields into a FHIR JSON object.
func applyFields(base map[string]any, fields map[string]any) error {
	for key, value := range fields {
		if strings.Contains(key, ".") {
			if err := setNestedField(base, key, value); err != nil {
				return err
			}
			continue
		}
		base[key] = value
	}
	return nil
}

func setNestedField(root map[string]any, path string, value any) error {
	parts := strings.Split(path, ".")
	current := any(root)
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		asMap, ok := current.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: cannot traverse %q", ErrInvalidInput, path)
		}
		next, ok := asMap[part]
		if !ok {
			child := map[string]any{}
			asMap[part] = child
			current = child
			continue
		}
		if childMap, ok := next.(map[string]any); ok {
			current = childMap
			continue
		}
		return fmt.Errorf("%w: cannot traverse %q", ErrInvalidInput, path)
	}
	asMap, ok := current.(map[string]any)
	if !ok {
		return fmt.Errorf("%w: cannot set %q", ErrInvalidInput, path)
	}
	asMap[parts[len(parts)-1]] = value
	return nil
}

func envelopeJSON(resourceType string, id string, fields map[string]any) ([]byte, error) {
	root := map[string]any{"resourceType": resourceType}
	if id != "" {
		root["id"] = id
	}
	if err := applyFields(root, fields); err != nil {
		return nil, err
	}
	return json.Marshal(root)
}

func filterResourceJSON(data []byte, allowedFields []string) (map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(allowedFields) == 0 {
		return root, nil
	}
	filtered := map[string]any{
		"resourceType": root["resourceType"],
	}
	if id, ok := root["id"]; ok {
		filtered["id"] = id
	}
	for _, field := range allowedFields {
		if v, ok := root[field]; ok {
			filtered[field] = v
		}
	}
	return filtered, nil
}

// filterSearchResourceJSON applies the safe search projection. Search results
// expose only resourceType/id unless fields are explicitly allowed; callers
// must opt into the full resource payload with AllowAllFields.
func filterSearchResourceJSON(data []byte, allowedFields []string, allowAll bool) (map[string]any, error) {
	if allowAll {
		return filterResourceJSON(data, nil)
	}
	if len(allowedFields) > 0 {
		return filterResourceJSON(data, allowedFields)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return map[string]any{
		"resourceType": root["resourceType"],
		"id":           root["id"],
	}, nil
}
