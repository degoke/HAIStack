package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type builtinEngine struct {
	protoCodec         proto.ProtoCodec
	knownResourceTypes map[string]struct{}
	installedTypes     ResourceTypeRegistry
	requiredFields     map[string][]string
	profileCatalog     ProfileCatalog
	fhirpath           fhirpath.Engine
}

// NewEngine returns the built-in haistack-validate engine.
func NewEngine(cfg Config) (Engine, error) {
	if cfg.ProtoCodec == nil {
		cfg.ProtoCodec = proto.NewGoogleR4Codec()
	}
	if cfg.KnownResourceTypes == nil {
		cfg.KnownResourceTypes = proto.KnownR4ResourceTypes()
	}
	cfg.RequiredFields = mergeRequiredFields(cfg.RequiredFields)
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &builtinEngine{
		protoCodec:         cfg.ProtoCodec,
		knownResourceTypes: cfg.KnownResourceTypes,
		installedTypes:     cfg.InstalledTypes,
		requiredFields:     cfg.RequiredFields,
		profileCatalog:     cfg.ProfileCatalog,
		fhirpath:           cfg.FHIRPath,
	}, nil
}

func (e *builtinEngine) Validate(ctx context.Context, res *types.ResourceEnvelope, opts ValidateOptions) (*ValidationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if res == nil {
		return invalidResult(issue("invalid", "resource is nil", nil)), nil
	}
	if len(res.JSON) == 0 {
		return invalidResult(issue("invalid", "resource JSON is empty", nil)), nil
	}

	var issues []ValidationIssue

	obj, release, jsonErr := decodeJSONObject(res.JSON)
	if release != nil {
		defer release()
	}
	if jsonErr != nil {
		issues = append(issues, issue("invalid-json", jsonErr.Error(), nil))
		return &ValidationResult{Valid: false, Issues: issues}, nil
	}

	resourceType, rtErr := types.GetResourceType(res.JSON)
	if rtErr != nil {
		issues = append(issues, issue("missing-resource-type", rtErr.Error(), []string{"Resource.resourceType"}))
	} else {
		if _, known := e.knownResourceTypes[resourceType]; !known {
			issues = append(issues, issue(
				"unknown-resource-type",
				fmt.Sprintf("unknown FHIR resource type %q", resourceType),
				[]string{"Resource.resourceType"},
			))
		}

		registry := opts.ResourceTypeRegistry
		if registry == nil {
			registry = e.installedTypes
		}
		if registry != nil {
			if _, known := e.knownResourceTypes[resourceType]; known && !registry.IsInstalled(resourceType) {
				issues = append(issues, issue(
					"resource-type-not-installed",
					fmt.Sprintf("resource type %q is not installed", resourceType),
					[]string{"Resource.resourceType"},
				))
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	id, idErr := types.GetID(res.JSON)
	if idErr != nil {
		issues = append(issues, issue("invalid-id", idErr.Error(), []string{"Resource.id"}))
	} else {
		switch {
		case id == "" && opts.RequireID:
			issues = append(issues, issue("missing-id", "id is required", []string{"Resource.id"}))
		case id != "" && !fhirIDPattern.MatchString(id):
			issues = append(issues, issue(
				"invalid-id",
				fmt.Sprintf("id %q does not match FHIR id syntax", id),
				[]string{"Resource.id"},
			))
		}
	}

	if resourceType != "" {
		if required, ok := e.requiredFields[resourceType]; ok {
			for _, field := range required {
				if !hasTopLevelField(obj, field) {
					issues = append(issues, issue(
						"missing-required-field",
						fmt.Sprintf("required field %q is missing for %s", field, resourceType),
						[]string{resourceType + "." + field},
					))
				}
			}
		}
	}

	if opts.ReferencePolicy == ReferencePolicySyntactic || opts.ReferencePolicy == 0 {
		refs, refErr := types.GetReferences(res.JSON)
		if refErr != nil {
			issues = append(issues, issue("invalid-json", refErr.Error(), nil))
		} else {
			for _, ref := range refs {
				if refIssue := validateReferenceSyntax(ref); refIssue != nil {
					issues = append(issues, *refIssue)
				}
			}
		}
	}

	if shouldRunProtoValidation(resourceType, e.knownResourceTypes, issues) {
		if err := e.validateStructure(ctx, resourceType, res); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			issues = append(issues, issue(
				"structural",
				structuralDiagnostics(err),
				[]string{resourceType},
			))
		}
	}
	if opts.TerminologyEnabled && opts.Terminology != nil {
		e.validateTerminology(ctx, obj, resourceType, opts, &issues)
	}
	if opts.ProfileCatalog != nil || e.profileCatalog != nil {
		if opts.EnforceBaseProfile || opts.EnforceDeclaredProfiles || len(opts.Profiles) > 0 {
			e.validateProfiles(ctx, res, obj, resourceType, opts, &issues)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
	}

	if len(issues) > 0 {
		return &ValidationResult{Valid: !hasErrorIssues(issues), Issues: issues}, nil
	}
	return &ValidationResult{Valid: true}, nil
}

func (e *builtinEngine) validateTerminology(ctx context.Context, obj map[string]interface{}, resourceType string, opts ValidateOptions, issues *[]ValidationIssue) {
	for path, binding := range opts.TerminologyBindings {
		value, ok := obj[path]
		if !ok || value == nil {
			continue
		}
		codings := codedValues(value)
		if len(codings) == 0 {
			continue
		}
		valid := false
		unknown := false
		invalid := false
		for _, c := range codings {
			r, err := opts.Terminology.ValidateCode(ctx, terminology.ValidateCodeRequest{URL: binding.URL, Version: binding.Version, Coding: c})
			if err != nil {
				*issues = append(*issues, ValidationIssue{Severity: "warning", Code: "terminology-unavailable", Diagnostics: err.Error(), Expression: []string{resourceType + "." + path}})
				continue
			}
			switch r.Status {
			case terminology.Valid:
				valid = true
				if r.DisplayWarning {
					*issues = append(*issues, ValidationIssue{Severity: "warning", Code: "terminology-display-warning", Diagnostics: r.Message, Expression: []string{resourceType + "." + path}})
				}
			case terminology.UnknownTerminology:
				unknown = true
			case terminology.Invalid:
				invalid = true
				severity := "warning"
				if binding.Strength == "required" {
					severity = "error"
				}
				*issues = append(*issues, ValidationIssue{Severity: severity, Code: "terminology-invalid", Diagnostics: r.Message, Expression: []string{resourceType + "." + path}})
			}
		}
		if !valid && unknown && binding.Strength == "required" {
			*issues = append(*issues, issue("terminology-unknown", "terminology is unknown", []string{resourceType + "." + path}))
		}
		if !valid && !unknown && !invalid && binding.Strength == "required" {
			*issues = append(*issues, issue("terminology-invalid", "no coding is valid", []string{resourceType + "." + path}))
		}
	}
}

func hasErrorIssues(issues []ValidationIssue) bool {
	for _, i := range issues {
		if i.Severity != "warning" {
			return true
		}
	}
	return false
}

func codedValues(v interface{}) []terminology.Coding {
	var out []terminology.Coding
	switch x := v.(type) {
	case map[string]interface{}:
		if s, _ := x["system"].(string); s != "" {
			c, _ := x["code"].(string)
			d, _ := x["display"].(string)
			ver, _ := x["version"].(string)
			out = append(out, terminology.Coding{System: s, Code: c, Display: d, Version: ver})
		}
		if cs, ok := x["coding"].([]interface{}); ok {
			for _, c := range cs {
				out = append(out, codedValues(c)...)
			}
		}
	case []interface{}:
		for _, c := range x {
			out = append(out, codedValues(c)...)
		}
	}
	return out
}

func shouldRunProtoValidation(resourceType string, known map[string]struct{}, issues []ValidationIssue) bool {
	if resourceType == "" {
		return false
	}
	if _, ok := known[resourceType]; !ok {
		return false
	}
	for _, iss := range issues {
		switch iss.Code {
		case "unknown-resource-type", "resource-type-not-installed", "missing-resource-type", "invalid-json":
			return false
		}
	}
	return true
}

func decodeJSONObject(data []byte) (map[string]interface{}, func(), error) {
	obj := jsonObjectPool.Get().(map[string]interface{})
	clear(obj)
	if err := json.Unmarshal(data, &obj); err != nil {
		jsonObjectPool.Put(obj)
		return nil, nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return obj, func() {
		clear(obj)
		jsonObjectPool.Put(obj)
	}, nil
}

var jsonObjectPool = sync.Pool{
	New: func() any {
		return make(map[string]interface{})
	},
}

func hasTopLevelField(obj map[string]interface{}, field string) bool {
	value, ok := obj[field]
	if !ok {
		return false
	}
	if value == nil {
		return false
	}
	return true
}

func issue(code, diagnostics string, expression []string) ValidationIssue {
	return ValidationIssue{
		Severity:    "error",
		Code:        code,
		Diagnostics: diagnostics,
		Expression:  expression,
	}
}

func invalidResult(iss ValidationIssue) *ValidationResult {
	return &ValidationResult{Valid: false, Issues: []ValidationIssue{iss}}
}

func joinIssueDiagnostics(issues []ValidationIssue) string {
	parts := make([]string, 0, len(issues))
	for _, iss := range issues {
		if strings.TrimSpace(iss.Diagnostics) != "" {
			parts = append(parts, iss.Diagnostics)
		}
	}
	return strings.Join(parts, "; ")
}
