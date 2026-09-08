package sdc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ResourceServiceReferenceResolver resolves references through a ResourceService.
type ResourceServiceReferenceResolver struct {
	Service ResourceService
}

func (r ResourceServiceReferenceResolver) ResolveReference(ctx context.Context, ref Reference) (map[string]any, error) {
	if r.Service == nil {
		return nil, fmt.Errorf("resource service is unavailable")
	}
	resourceType := ref.Type
	resourceID := ""
	if ref.Reference != "" {
		parts := strings.Split(strings.TrimPrefix(ref.Reference, "#"), "/")
		if len(parts) >= 2 {
			resourceType = parts[0]
			resourceID = parts[1]
		}
	}
	if resourceType == "" || resourceID == "" {
		return nil, fmt.Errorf("reference cannot be resolved: %s", ref.Reference)
	}
	env, err := r.Service.Read(ctx, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(env.JSON, &out); err != nil {
		return nil, err
	}
	return out, nil
}
