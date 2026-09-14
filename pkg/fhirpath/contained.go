package fhirpath

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ResolveContainedReference resolves a #fragment against a containing resource map or envelope.
func ResolveContainedReference(parent any, fragment string) (any, bool) {
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
	case *types.ResourceEnvelope:
		if len(env.JSON) > 0 {
			var asMap map[string]any
			if err := json.Unmarshal(env.JSON, &asMap); err == nil {
				return findContainedInMap(asMap, targetID)
			}
		}
		if env.Proto != nil {
			data, err := json.Marshal(env.Proto)
			if err == nil {
				var asMap map[string]any
				if err := json.Unmarshal(data, &asMap); err == nil {
					return findContainedInMap(asMap, targetID)
				}
			}
		}
		return nil, false
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

// ResolveContainedReferenceAsEnvelope resolves a contained reference to a ResourceEnvelope.
func ResolveContainedReferenceAsEnvelope(parent any, fragment string) (*types.ResourceEnvelope, bool) {
	resolved, ok := ResolveContainedReference(parent, fragment)
	if !ok {
		return nil, false
	}
	if env, ok := resolved.(*types.ResourceEnvelope); ok {
		return env, true
	}
	asMap, ok := resolved.(map[string]any)
	if !ok {
		return nil, false
	}
	resourceType, _ := asMap["resourceType"].(string)
	if resourceType == "" {
		return nil, false
	}
	data, err := json.Marshal(asMap)
	if err != nil {
		return nil, false
	}
	env, err := types.NewJSONCodec().ParseJSON(resourceType, data)
	if err != nil {
		return nil, false
	}
	return env, true
}

// WrapResolveWithEvaluationResource enables resolve() for contained #fragment references.
func WrapResolveWithEvaluationResource(fn ResolveFunc) ResolveFunc {
	return func(ctx context.Context, ref string) (any, error) {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(ref, "#") {
			if parent := EvaluationResource(ctx); parent != nil {
				if resolved, ok := ResolveContainedReferenceAsEnvelope(parent, ref); ok {
					return resolved, nil
				}
			}
			return nil, nil
		}
		if fn == nil {
			return nil, nil
		}
		return fn(ctx, ref)
	}
}

// LookupLogicalIDAcrossTypes tries Read for each resource type until one succeeds.
func LookupLogicalIDAcrossTypes(
	read func(ctx context.Context, resourceType, id string) (any, error),
	resourceTypes []string,
) func(ctx context.Context, logicalID string) (resourceType, id string, ok bool) {
	return func(ctx context.Context, logicalID string) (string, string, bool) {
		logicalID = strings.TrimSpace(logicalID)
		if logicalID == "" || read == nil {
			return "", "", false
		}
		for _, rt := range resourceTypes {
			if _, err := read(ctx, rt, logicalID); err == nil {
				return rt, logicalID, true
			}
		}
		return "", "", false
	}
}

// DefaultLogicalIDResourceTypes lists common FHIR resource types for urn:uuid lookup.
var DefaultLogicalIDResourceTypes = []string{
	"Patient", "Practitioner", "RelatedPerson", "Organization", "Location",
	"Encounter", "Appointment", "Observation", "Condition", "Procedure",
	"Device", "Medication", "MedicationRequest", "DiagnosticReport",
}
