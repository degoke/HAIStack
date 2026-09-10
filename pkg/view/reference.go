package view

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

func referenceStringFromValue(v fhirpath.Value) (string, bool) {
	switch val := v.Raw().(type) {
	case *dtpb.Reference:
		return referenceStringFromProto(val)
	default:
		s, err := v.String()
		if err != nil || strings.TrimSpace(s) == "" {
			return "", false
		}
		return strings.TrimSpace(s), true
	}
}

func referenceStringFromProto(ref *dtpb.Reference) (string, bool) {
	return fhirpath.ReferenceStringFromProto(ref)
}

func (e *Executor) resolveIterationContext(ctx context.Context, item fhirpath.Value, parent any) (any, error) {
	raw := item.Raw()
	if raw == nil {
		return nil, nil
	}
	if ref, ok := raw.(*dtpb.Reference); ok {
		return e.resolveReferenceProto(ctx, ref, parent)
	}
	if refStr, ok := referenceStringFromValue(item); ok {
		return e.resolveReferenceString(ctx, refStr, parent)
	}
	return raw, nil
}

func (e *Executor) resolveReferenceProto(ctx context.Context, ref *dtpb.Reference, parent any) (any, error) {
	refStr, ok := referenceStringFromProto(ref)
	if !ok {
		return ref, nil
	}
	return e.resolveReferenceString(ctx, refStr, parent)
}

func (e *Executor) resolveReferenceString(ctx context.Context, refStr string, parent any) (any, error) {
	refStr = strings.TrimSpace(refStr)
	if refStr == "" {
		return nil, nil
	}
	if strings.HasPrefix(refStr, "#") {
		if resolved, ok := resolveContainedReference(parent, refStr); ok {
			return resolved, nil
		}
		return nil, nil
	}
	resourceType, id, ok := fhirpath.ParseReferenceForRead(refStr, e.cfg.BaseURL)
	if !ok {
		return nil, nil
	}
	if resourceType == "" {
		if e.cfg.ResolveLogicalID != nil {
			resolvedType, resolvedID, found := e.cfg.ResolveLogicalID(ctx, id)
			if found {
				resourceType, id = resolvedType, resolvedID
			}
		}
		if resourceType == "" {
			return nil, nil
		}
	}
	env, err := e.cfg.Resources.Read(ctx, resourceType, id)
	if err != nil {
		return nil, fmt.Errorf("resolve reference %q: %w", refStr, err)
	}
	return env, nil
}

func resolveContainedReference(parent any, fragment string) (any, bool) {
	if parent == nil {
		return nil, false
	}
	targetID := strings.TrimPrefix(strings.TrimSpace(fragment), "#")
	if targetID == "" {
		return nil, false
	}
	switch env := parent.(type) {
	case map[string]any:
		return findContainedInMap(env, targetID)
	default:
		data, err := json.Marshal(parent)
		if err != nil {
			return nil, false
		}
		var asMap map[string]any
		if err := json.Unmarshal(data, &asMap); err != nil {
			return nil, false
		}
		return findContainedInMap(asMap, targetID)
	}
}

func findContainedInMap(resource map[string]any, targetID string) (any, bool) {
	if id, _ := resource["id"].(string); id == targetID {
		return resource, true
	}
	contained, ok := resource["contained"].([]any)
	if !ok {
		return nil, false
	}
	for _, item := range contained {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := entry["id"].(string); id == targetID {
			return entry, true
		}
	}
	return nil, false
}
