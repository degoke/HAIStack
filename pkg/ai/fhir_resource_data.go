package ai

import (
	"encoding/json"
	"fmt"
)

// resourceJSONBytes reports whether data is raw JSON that can be scrubbed without
// first decoding to map[string]any.
func resourceJSONBytes(data any) ([]byte, bool) {
	switch v := data.(type) {
	case []byte:
		return v, len(v) > 0
	case json.RawMessage:
		return []byte(v), len(v) > 0
	case string:
		if v == "" {
			return nil, false
		}
		return []byte(v), true
	default:
		return nil, false
	}
}

// resourceDataAsMap decodes tool output into a mutable resource map when possible.
func resourceDataAsMap(data any) (map[string]any, error) {
	switch v := data.(type) {
	case nil:
		return nil, fmt.Errorf("deidentify: nil data")
	case map[string]any:
		return v, nil
	case []byte:
		return unmarshalJSONMap(v)
	case json.RawMessage:
		return unmarshalJSONMap([]byte(v))
	case string:
		return unmarshalJSONMap([]byte(v))
	default:
		return nil, fmt.Errorf("deidentify: unsupported data type %T", data)
	}
}

func unmarshalJSONMap(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("deidentify: empty JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("deidentify: invalid JSON: %w", err)
	}
	if m == nil {
		return nil, fmt.Errorf("deidentify: JSON root is not an object")
	}
	return m, nil
}
