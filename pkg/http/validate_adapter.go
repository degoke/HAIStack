package http

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
)

// CoreValidateService implements FHIR Resource/$validate using validate.Engine.
type CoreValidateService struct {
	Engine    validate.Engine
	Resources ResourceService
	Codec     types.ResourceCodec
	Options   validate.ValidateOptions
	Profiles  []string
}

// Validate runs profile validation and returns a FHIR OperationOutcome.
func (s CoreValidateService) Validate(ctx context.Context, req ValidateRequest) (*types.OperationOutcome, error) {
	if s.Engine == nil {
		return nil, &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: "validate service is unavailable"}
	}
	if req.ResourceType == "" {
		return nil, invalidRequest("resource type is required", nil)
	}
	mode := strings.TrimSpace(req.Query.Get("mode"))
	if mode == "delete" {
		return s.validateDelete(ctx, req)
	}

	codec := s.Codec
	if codec == nil {
		codec = types.NewJSONCodec()
	}

	envelope, profiles, err := parseValidateInput(codec, req.ResourceType, req.ContentType, req.Body)
	if err != nil {
		return nil, err
	}
	if envelope == nil {
		if req.ID == "" {
			return nil, invalidRequest("resource input is required", nil)
		}
		if s.Resources == nil {
			return nil, &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: "resource service is unavailable"}
		}
		envelope, err = s.Resources.Read(ctx, req.ResourceType, req.ID)
		if err != nil {
			return nil, err
		}
	}
	if req.ID != "" && envelope.ID != "" && envelope.ID != req.ID {
		return nil, idMismatch(req.ID, envelope.ID)
	}
	if req.ID != "" && envelope.ID == "" {
		envelope.ID = req.ID
	}
	if envelope.ResourceType != "" && envelope.ResourceType != req.ResourceType {
		return nil, invalidRequest(
			fmt.Sprintf("resource type %q does not match path %q", envelope.ResourceType, req.ResourceType),
			nil,
			"Resource.resourceType",
		)
	}

	opts := s.Options
	opts.Profiles = append(append([]string(nil), s.Profiles...), profiles...)
	if queryProfile := strings.TrimSpace(req.Query.Get("profile")); queryProfile != "" {
		opts.Profiles = append(opts.Profiles, queryProfile)
	}
	if truthy(req.Query.Get("_full")) {
		opts.Mode = validate.ValidationModeFull
		opts.ProfileConstraints = true
	}
	if truthy(req.Query.Get("_invariants")) {
		opts.ProfileConstraints = true
	}
	if !opts.ProfileConstraints && opts.Mode != validate.ValidationModeFull {
		// Explicit validate calls default to running FHIRPath invariants unless
		// the caller opts into the lightweight runtime write profile.
		opts.ProfileConstraints = true
	}

	result, err := s.Engine.Validate(ctx, envelope, opts)
	if err != nil {
		return nil, err
	}
	return validate.ToOperationOutcome(result), nil
}

func (s CoreValidateService) validateDelete(ctx context.Context, req ValidateRequest) (*types.OperationOutcome, error) {
	if req.ID == "" {
		return nil, invalidRequest("delete validation requires a resource instance", nil)
	}
	if s.Resources == nil {
		return nil, &core.ServiceError{Kind: core.ErrorKindNotSupported, Message: "resource service is unavailable"}
	}
	if _, err := s.Resources.Read(ctx, req.ResourceType, req.ID); err != nil {
		return nil, err
	}
	return &types.OperationOutcome{ResourceType: "OperationOutcome"}, nil
}

func truthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

type validateParameters struct {
	ResourceType string `json:"resourceType"`
	Parameter    []struct {
		Name           string          `json:"name"`
		ValueCode      string          `json:"valueCode,omitempty"`
		ValueCanonical string          `json:"valueCanonical,omitempty"`
		ValueUri       string          `json:"valueUri,omitempty"`
		Resource       json.RawMessage `json:"resource,omitempty"`
	} `json:"parameter"`
}

func parseValidateInput(codec types.ResourceCodec, resourceType, contentType string, body []byte) (*types.ResourceEnvelope, []string, error) {
	if len(body) == 0 {
		return nil, nil, nil
	}
	data, _, err := requestBodyJSON(contentType, body)
	if err != nil {
		return nil, nil, invalidRequest("parse validate input", err)
	}
	var peek struct {
		ResourceType string `json:"resourceType"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, nil, invalidRequest("parse validate input", err)
	}
	if peek.ResourceType == "Parameters" {
		return parseValidateParameters(codec, resourceType, data)
	}
	envelope, err := codec.ParseJSON(resourceType, data)
	if err != nil {
		return nil, nil, invalidRequest("parse validate resource", err)
	}
	return envelope, nil, nil
}

func parseValidateParameters(codec types.ResourceCodec, resourceType string, data []byte) (*types.ResourceEnvelope, []string, error) {
	var params validateParameters
	if err := json.Unmarshal(data, &params); err != nil {
		return nil, nil, invalidRequest("parse Parameters", err)
	}
	var (
		envelope *types.ResourceEnvelope
		profiles []string
	)
	for _, param := range params.Parameter {
		switch param.Name {
		case "resource":
			if len(param.Resource) == 0 {
				continue
			}
			parsed, err := codec.ParseJSON(resourceType, param.Resource)
			if err != nil {
				return nil, nil, invalidRequest("parse Parameters.resource", err)
			}
			envelope = parsed
		case "profile":
			if profile := strings.TrimSpace(param.ValueCanonical); profile != "" {
				profiles = append(profiles, profile)
			} else if profile := strings.TrimSpace(param.ValueUri); profile != "" {
				profiles = append(profiles, profile)
			}
		case "mode":
			if code := strings.TrimSpace(param.ValueCode); code == "delete" {
				return nil, nil, invalidRequest("delete mode requires an instance-level validate URL", nil)
			}
		}
	}
	return envelope, profiles, nil
}
