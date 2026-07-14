package auth

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyFormat identifies a policy document encoding.
type PolicyFormat string

const (
	PolicyFormatJSON PolicyFormat = "json"
	PolicyFormatYAML PolicyFormat = "yaml"
	PolicyFormatAuto PolicyFormat = "auto"
)

// ParsePolicy parses a policy document from JSON or YAML bytes.
func ParsePolicy(data []byte, format PolicyFormat) (PolicyDocument, error) {
	if len(data) == 0 {
		return PolicyDocument{}, fmt.Errorf("%w: empty document", ErrInvalidPolicy)
	}
	f := format
	if f == "" || f == PolicyFormatAuto {
		f = detectPolicyFormat(data)
	}
	var doc PolicyDocument
	switch f {
	case PolicyFormatJSON:
		if err := json.Unmarshal(data, &doc); err != nil {
			return PolicyDocument{}, fmt.Errorf("%w: %v", ErrInvalidPolicy, err)
		}
	case PolicyFormatYAML:
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return PolicyDocument{}, fmt.Errorf("%w: %v", ErrInvalidPolicy, err)
		}
	default:
		return PolicyDocument{}, fmt.Errorf("%w: unsupported format %q", ErrInvalidPolicy, f)
	}
	return doc, nil
}

// ParseAndCompilePolicy parses and compiles a policy document in one step.
func ParseAndCompilePolicy(data []byte, format PolicyFormat) (*CompiledPolicy, error) {
	doc, err := ParsePolicy(data, format)
	if err != nil {
		return nil, err
	}
	return CompilePolicy(doc)
}

func detectPolicyFormat(data []byte) PolicyFormat {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return PolicyFormatJSON
	}
	return PolicyFormatYAML
}
