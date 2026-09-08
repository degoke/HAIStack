package sdc

import (
	"encoding/json"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// OperationParameters carries subject and launch context for SDC operations.
type OperationParameters struct {
	Subject       any
	LaunchContext map[string]any
}

// ParseOperationParameters extracts SDC populate/validate parameters from a FHIR Parameters resource.
func ParseOperationParameters(env *types.ResourceEnvelope) OperationParameters {
	out := OperationParameters{LaunchContext: map[string]any{}}
	if env == nil || env.ResourceType != "Parameters" || len(env.JSON) == 0 {
		return out
	}
	var params struct {
		Parameter []struct {
			Name           string          `json:"name"`
			ValueReference map[string]any  `json:"valueReference,omitempty"`
			Resource       json.RawMessage `json:"resource,omitempty"`
			Part           []struct {
				Name           string          `json:"name"`
				ValueReference map[string]any  `json:"valueReference,omitempty"`
				Resource       json.RawMessage `json:"resource,omitempty"`
			} `json:"part,omitempty"`
		} `json:"parameter"`
	}
	if json.Unmarshal(env.JSON, &params) != nil {
		return out
	}
	for _, param := range params.Parameter {
		switch param.Name {
		case "subject":
			if param.ValueReference != nil {
				out.Subject = param.ValueReference
			} else if len(param.Resource) > 0 {
				var resource map[string]any
				if json.Unmarshal(param.Resource, &resource) == nil {
					out.Subject = resource
				}
			}
		case "launchContext":
			if len(param.Resource) > 0 {
				var resource map[string]any
				if json.Unmarshal(param.Resource, &resource) == nil {
					if name := launchContextName(param.Part); name != "" {
						out.LaunchContext[name] = resource
					}
				}
			}
			for _, part := range param.Part {
				if part.Name == "name" {
					continue
				}
				if len(part.Resource) > 0 {
					var resource map[string]any
					if json.Unmarshal(part.Resource, &resource) == nil {
						if name := launchContextName(param.Part); name != "" {
							out.LaunchContext[name] = resource
						}
					}
				}
			}
		default:
			if len(param.Resource) > 0 {
				var resource map[string]any
				if json.Unmarshal(param.Resource, &resource) == nil {
					out.LaunchContext[param.Name] = resource
				}
			} else if param.ValueReference != nil {
				out.LaunchContext[param.Name] = param.ValueReference
			}
		}
	}
	return out
}

func launchContextName(parts []struct {
	Name           string          `json:"name"`
	ValueReference map[string]any  `json:"valueReference,omitempty"`
	Resource       json.RawMessage `json:"resource,omitempty"`
}) string {
	for _, part := range parts {
		if part.Name == "name" {
			if code, ok := part.ValueReference["reference"].(string); ok {
				return code
			}
		}
	}
	return ""
}

func ValidationOptionsFromParameters(params OperationParameters, base ValidationOptions) ValidationOptions {
	if params.Subject != nil {
		base.Subject = params.Subject
	}
	if len(params.LaunchContext) > 0 {
		if base.LaunchContext == nil {
			base.LaunchContext = map[string]any{}
		}
		for k, v := range params.LaunchContext {
			base.LaunchContext[k] = v
		}
	}
	return base
}

func PopulationContextFromParameters(params OperationParameters, base PopulationContext) PopulationContext {
	if params.Subject != nil {
		base.Subject = params.Subject
	}
	if len(params.LaunchContext) > 0 {
		if base.LaunchContext == nil {
			base.LaunchContext = map[string]any{}
		}
		for k, v := range params.LaunchContext {
			base.LaunchContext[k] = v
		}
	}
	return base
}

// AssemblerFromParameters builds an assembler using named parameters as assemble context.
func AssemblerFromParameters(params OperationParameters, resolver QuestionnaireResolver, elements DefinitionElementResolver) Assembler {
	context := map[string]any{}
	for k, v := range params.LaunchContext {
		context[k] = v
	}
	if params.Subject != nil {
		context["subject"] = params.Subject
	}
	return Assembler{Resolver: resolver, Context: context, Elements: elements}
}
