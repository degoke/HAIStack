package parquetfhir

import (
	"encoding/json"
	"fmt"
)

// PrepareRow normalizes one FHIR resource map for parquet writing and adds
// Parquet-on-FHIR annotation columns.
func PrepareRow(raw map[string]any, index *elementIndex) (map[string]any, error) {
	if raw == nil {
		return nil, fmt.Errorf("parquetfhir: nil resource")
	}
	row := make(map[string]any, len(raw))
	for key, value := range raw {
		if value == nil || isAnnotationField(key) {
			continue
		}
		normalized, err := normalizeValue(value)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", key, err)
		}
		if normalized != nil {
			row[key] = normalized
		}
	}
	resourceType, _ := row["resourceType"].(string)
	if resourceType == "" {
		if rt, ok := raw["resourceType"].(string); ok && rt != "" {
			row["resourceType"] = rt
		} else {
			return nil, fmt.Errorf("parquetfhir: resourceType is required")
		}
	}
	if index != nil {
		if err := enrichAnnotations(row, index); err != nil {
			return nil, err
		}
	}
	return row, nil
}

func normalizeValue(value any) (any, error) {
	switch v := value.(type) {
	case bool, string, int, int32, int64, float32, float64:
		return v, nil
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, nil
		}
		return v.String(), nil
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			if item == nil {
				continue
			}
			normalized, err := normalizeValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, normalized)
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if item == nil || isAnnotationField(key) {
				continue
			}
			normalized, err := normalizeValue(item)
			if err != nil {
				return nil, err
			}
			if normalized != nil {
				out[key] = normalized
			}
		}
		return out, nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	}
}
