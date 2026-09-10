package view

import (
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

// RegisterViewDefinition parses raw JSON and registers it when resourceType is ViewDefinition.
func RegisterViewDefinition(reg *Registry, raw []byte, engine fhirpath.Engine) (*ViewSpec, error) {
	if reg == nil {
		return nil, fmt.Errorf("view: registry is required")
	}
	if engine == nil {
		return nil, fmt.Errorf("view: engine is required")
	}
	var header struct {
		ResourceType string `json:"resourceType"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, fmt.Errorf("view: parse header: %w", err)
	}
	if header.ResourceType != "ViewDefinition" {
		return nil, fmt.Errorf("view: expected ViewDefinition, got %q", header.ResourceType)
	}
	return reg.Register(raw, engine)
}
