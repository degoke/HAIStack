package sdc

import (
	"context"
	"strings"
)

// ReferenceResolver resolves a reference answer for profile validation.
type ReferenceResolver interface {
	ResolveReference(context.Context, Reference) (map[string]any, error)
}

func validationOptionsWithContext(ctx context.Context, q Questionnaire, r QuestionnaireResponse, opts ValidationOptions) ValidationOptions {
	if opts.Expressions != nil {
		opts.Expressions = wrapExpressionProvider(ctx, q, r, opts, opts.Expressions)
	}
	return opts
}

func validateLaunchContexts(q Questionnaire, opts ValidationOptions, o *Outcome) {
	merged := mergeLaunchContext(q.LaunchContexts, opts.LaunchContext)
	for _, lc := range q.LaunchContexts {
		if lc.Name == "" {
			continue
		}
		val := merged[lc.Name]
		if val == nil {
			o.add("error", "required", "launch context "+lc.Name+" is required", "Questionnaire.extension.launchContext")
			continue
		}
		if len(lc.Type) == 0 {
			continue
		}
		rt := resourceTypeFromLaunchValue(val)
		if rt == "" {
			o.add("error", "invalid", "launch context "+lc.Name+" has no resource type", "Questionnaire.extension.launchContext")
			continue
		}
		allowed := false
		for _, want := range lc.Type {
			if strings.EqualFold(want, rt) {
				allowed = true
				break
			}
		}
		if !allowed {
			o.add("error", "invalid", "launch context "+lc.Name+" must be type "+strings.Join(lc.Type, " or "), "Questionnaire.extension.launchContext")
		}
	}
}

func resourceTypeFromLaunchValue(value any) string {
	switch x := value.(type) {
	case map[string]any:
		if rt, ok := x["resourceType"].(string); ok && rt != "" {
			return rt
		}
		if ref, ok := x["reference"].(string); ok {
			return resourceTypeFromReference(ref)
		}
	case Reference:
		if x.Type != "" {
			return x.Type
		}
		return resourceTypeFromReference(x.Reference)
	}
	return ""
}

func profilesFromResource(resource map[string]any) []string {
	meta, _ := resource["meta"].(map[string]any)
	if meta == nil {
		return nil
	}
	raw := meta["profile"]
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{v}
	default:
		return nil
	}
}

func profileMatches(want, got string) bool {
	if want == got {
		return true
	}
	wantBase, _, _ := strings.Cut(want, "|")
	gotBase, _, _ := strings.Cut(got, "|")
	return wantBase != "" && wantBase == gotBase
}
