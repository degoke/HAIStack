package bulkimport

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseParametersKickoff decodes a FHIR Parameters resource into a kickoff request.
// Each repeating `input` parameter must include a `type` part and either inline
// NDJSON (`valueString` / `ndjson`) or a `url` part.
func ParseParametersKickoff(data []byte) (KickoffRequest, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return KickoffRequest{}, fmt.Errorf("import: parse Parameters: %w", err)
	}
	if rt, _ := raw["resourceType"].(string); rt != "" && rt != "Parameters" {
		return KickoffRequest{}, fmt.Errorf("import: expected Parameters resource, got %s", rt)
	}
	req := KickoffRequest{InputFormat: InputFormatNDJSON}
	params, _ := raw["parameter"].([]any)
	for _, item := range params {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := obj["name"].(string)
		switch name {
		case "inputFormat":
			if v := parameterString(obj); v != "" {
				req.InputFormat = v
			}
		case "input":
			input, err := parseInputParameter(obj)
			if err != nil {
				return KickoffRequest{}, err
			}
			req.Inputs = append(req.Inputs, input)
		}
	}
	if len(req.Inputs) == 0 {
		return KickoffRequest{}, fmt.Errorf("import: Parameters must include at least one input")
	}
	return req, nil
}

func parseInputParameter(obj map[string]any) (InputFile, error) {
	input := InputFile{}
	parts, _ := obj["part"].([]any)
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := part["name"].(string)
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "type":
			input.Type = parameterString(part)
		case "url":
			input.URL = parameterString(part)
		case "valuestring", "ndjson", "resource":
			input.NDJSON = []byte(parameterString(part))
		}
	}
	if input.Type == "" && len(input.NDJSON) > 0 {
		var peek struct {
			ResourceType string `json:"resourceType"`
		}
		first := input.NDJSON
		if i := indexByte(first, '\n'); i >= 0 {
			first = first[:i]
		}
		_ = json.Unmarshal(first, &peek)
		input.Type = peek.ResourceType
	}
	if input.Type == "" && input.URL == "" && len(input.NDJSON) == 0 {
		return InputFile{}, fmt.Errorf("import: input parameter is missing type and payload")
	}
	return input, nil
}

func parameterString(obj map[string]any) string {
	for _, key := range []string{"valueCode", "valueString", "valueUri", "valueUrl", "valueCanonical"} {
		if v, ok := obj[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func indexByte(data []byte, c byte) int {
	for i, b := range data {
		if b == c {
			return i
		}
	}
	return -1
}
