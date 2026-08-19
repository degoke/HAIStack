package types

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// ResourceEnvelope is the generic runtime container for a FHIR resource.
// JSON and Hash are always canonical; Proto is optional (set by pkg/proto on proto paths).
type ResourceEnvelope struct {
	ResourceType string
	ID           string
	VersionID    string
	LastUpdated  time.Time
	JSON         []byte
	Proto        any
	Hash         string
}

// DecodeInto unmarshals the canonical resource JSON into a caller-owned typed
// FHIR struct. It keeps the generic envelope API while avoiding repeated
// manual json.Unmarshal calls in client applications.
func (e *ResourceEnvelope) DecodeInto(target any) error {
	if e == nil || len(e.JSON) == 0 {
		return fmt.Errorf("resource envelope JSON is empty")
	}
	if target == nil {
		return fmt.Errorf("decode target is nil")
	}
	return json.Unmarshal(e.JSON, target)
}

// Field returns a JSON field reached by the supplied object path. It is a
// lightweight alternative to decoding the entire envelope for callers that
// only need one or two values.
func (e *ResourceEnvelope) Field(path ...string) (any, bool) {
	if e == nil || len(e.JSON) == 0 || len(path) == 0 {
		return nil, false
	}
	var value any
	if err := json.Unmarshal(e.JSON, &value); err != nil {
		return nil, false
	}
	for _, part := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

// StringField returns a string-valued field from the envelope.
func (e *ResourceEnvelope) StringField(path ...string) (string, bool) {
	value, ok := e.Field(path...)
	if !ok {
		return "", false
	}
	result, ok := value.(string)
	return result, ok
}

// BoolField returns a boolean-valued field from the envelope.
func (e *ResourceEnvelope) BoolField(path ...string) (bool, bool) {
	value, ok := e.Field(path...)
	if !ok {
		return false, false
	}
	result, ok := value.(bool)
	return result, ok
}

// Int64Field returns an integer-valued field from the envelope.
func (e *ResourceEnvelope) Int64Field(path ...string) (int64, bool) {
	value, ok := e.Field(path...)
	if !ok {
		return 0, false
	}
	switch result := value.(type) {
	case float64:
		if result != float64(int64(result)) {
			return 0, false
		}
		return int64(result), true
	case json.Number:
		parsed, err := strconv.ParseInt(result.String(), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
