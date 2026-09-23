package ai

import (
	"context"

	"github.com/degoke/haistack/pkg/fhirpath"
	"github.com/degoke/haistack/pkg/types"
)

// buildEvalStringTargets evaluates compiled FHIRPath expressions once per
// resource (single marshal/parse) and collects string values to redact during
// the merged JSON walk.
func buildEvalStringTargets(
	ctx context.Context,
	engine fhirpath.Engine,
	resourceType string,
	root map[string]any,
	compiled []fhirpath.CompiledExpression,
) (map[string]struct{}, error) {
	if engine == nil || root == nil || len(compiled) == 0 {
		return nil, nil
	}
	resource, err := jsonResourceForFHIRPath(resourceType, root)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]struct{})
	for _, expr := range compiled {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		items, err := expr.Eval(ctx, resource)
		if err != nil || len(items) == 0 {
			continue
		}
		for _, item := range items {
			s, ok := primitiveStringFromValue(item)
			if !ok || s == "" {
				continue
			}
			targets[s] = struct{}{}
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	return targets, nil
}

func primitiveStringFromValue(v fhirpath.Value) (string, bool) {
	s, err := v.String()
	if err != nil {
		return "", false
	}
	return s, true
}

func jsonResourceForFHIRPath(resourceType string, root map[string]any) (any, error) {
	data, err := marshalJSONPooled(root)
	if err != nil {
		return nil, err
	}
	return types.NewJSONCodec().ParseJSON(resourceType, data)
}
