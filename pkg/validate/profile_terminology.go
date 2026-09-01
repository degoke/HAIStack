package validate

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/terminology"
)

func validateProfileTerminology(ctx context.Context, obj map[string]interface{}, sd *StructureDefinition, opts ValidateOptions, issues *[]ValidationIssue) {
	for _, el := range sd.Elements {
		if el.Binding == nil || el.Binding.ValueSet == "" {
			continue
		}
		if strings.EqualFold(el.Binding.Strength, "preferred") {
			continue
		}
		if strings.Contains(el.Path, ":") {
			continue
		}
		parent, _ := splitElementPath(el.Path)
		if parent != sd.Type && parent != "" && countPath(obj, parent) == 0 {
			continue
		}
		for _, value := range valuesAtPath(obj, el.Path) {
			if value == nil {
				continue
			}
			validateBoundValue(ctx, obj, sd.Type, el.Path, value, *el.Binding, opts, issues)
		}
	}
}

func validateBoundValue(ctx context.Context, obj map[string]interface{}, resourceType, path string, value interface{}, binding ElementBinding, opts ValidateOptions, issues *[]ValidationIssue) {
	codings := codedValues(value)
	if len(codings) == 0 {
		return
	}
	valid := false
	unknown := false
	invalid := false
	for _, c := range codings {
		r, err := opts.Terminology.ValidateCode(ctx, terminology.ValidateCodeRequest{
			ScopeID: "",
			URL:     binding.ValueSet,
			Version: binding.Version,
			Coding:  c,
		})
		if err != nil {
			*issues = append(*issues, ValidationIssue{
				Severity:    bindingSeverity(binding.Strength, "warning"),
				Code:        "terminology-unavailable",
				Diagnostics: err.Error(),
				Expression:  []string{path},
			})
			continue
		}
		switch r.Status {
		case terminology.Valid:
			valid = true
		case terminology.UnknownTerminology:
			unknown = true
		case terminology.Invalid:
			invalid = true
			*issues = append(*issues, ValidationIssue{
				Severity:    bindingSeverity(binding.Strength, "error"),
				Code:        "terminology-invalid",
				Diagnostics: r.Message,
				Expression:  []string{path},
			})
		}
	}
	strength := strings.ToLower(binding.Strength)
	if !valid && unknown && (strength == "required" || strength == "extensible") {
		*issues = append(*issues, issue(
			"terminology-unknown",
			fmt.Sprintf("%s: terminology %q is unknown", path, binding.ValueSet),
			[]string{path},
		))
	}
	if !valid && !unknown && !invalid && strength == "required" {
		_ = resourceType
		_ = obj
		*issues = append(*issues, issue(
			"terminology-invalid",
			fmt.Sprintf("%s: no coding is valid for binding %q", path, binding.ValueSet),
			[]string{path},
		))
	}
}

func bindingSeverity(strength, fallback string) string {
	switch strings.ToLower(strength) {
	case "required", "extensible":
		return "error"
	case "preferred":
		return "warning"
	default:
		return fallback
	}
}
