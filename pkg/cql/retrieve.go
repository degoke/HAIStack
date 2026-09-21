package cql

import (
	"context"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// StoreRetriever retrieves FHIR resources from a ResourceStore, filtered by
// the Patient subject when present.
type StoreRetriever struct {
	Resources store.ResourceStore
}

func (r StoreRetriever) Retrieve(ctx context.Context, req RetrieveRequest, patient any) ([]any, error) {
	if r.Resources == nil {
		return nil, errf("CQL retrieve store is unavailable")
	}
	if req.ResourceType == "" {
		return nil, errf("CQL retrieve is missing a resource type")
	}
	ids, err := r.Resources.ListIDs(ctx, req.ResourceType, 10000, 0)
	if err != nil {
		return nil, err
	}
	wantRef := patientReference(patient)
	var out []any
	for _, id := range ids {
		env, err := r.Resources.Read(ctx, req.ResourceType, id)
		if err != nil || env == nil {
			continue
		}
		if wantRef != "" && !resourceMatchesPatient(env, wantRef) {
			continue
		}
		if req.Terminology != "" && !resourceMatchesTerminology(env, req.Terminology) {
			continue
		}
		out = append(out, env)
	}
	return out, nil
}

// StaticRetriever returns a fixed collection, optionally filtered by resource type.
type StaticRetriever []any

func (r StaticRetriever) Retrieve(_ context.Context, req RetrieveRequest, _ any) ([]any, error) {
	if req.ResourceType == "" {
		return append([]any(nil), r...), nil
	}
	var out []any
	for _, item := range r {
		if resourceTypeOf(item) == req.ResourceType {
			out = append(out, item)
		}
	}
	return out, nil
}

func patientReference(patient any) string {
	if patient == nil {
		return ""
	}
	if env, ok := patient.(*types.ResourceEnvelope); ok && env != nil {
		if env.ResourceType != "" && env.ID != "" {
			return env.ResourceType + "/" + env.ID
		}
	}
	obj, ok := asObject(patient)
	if !ok {
		return ""
	}
	rt, _ := obj["resourceType"].(string)
	id, _ := obj["id"].(string)
	if rt != "" && id != "" {
		return rt + "/" + id
	}
	if id != "" {
		return "Patient/" + id
	}
	return ""
}

func resourceMatchesPatient(env *types.ResourceEnvelope, wantRef string) bool {
	if env == nil {
		return false
	}
	if env.ResourceType == "Patient" {
		got := env.ResourceType + "/" + env.ID
		return strings.EqualFold(got, wantRef) || env.ID == strings.TrimPrefix(wantRef, "Patient/")
	}
	obj, ok := asObject(env)
	if !ok {
		return false
	}
	subj, _ := obj["subject"].(map[string]any)
	if subj == nil {
		subj, _ = obj["patient"].(map[string]any)
	}
	if subj == nil {
		return false
	}
	ref, _ := subj["reference"].(string)
	if ref == "" {
		return false
	}
	return strings.EqualFold(ref, wantRef) || strings.HasSuffix(ref, "/"+strings.TrimPrefix(wantRef, "Patient/"))
}

func resourceMatchesTerminology(env *types.ResourceEnvelope, term string) bool {
	if env == nil || term == "" {
		return true
	}
	blob := string(env.JSON)
	return strings.Contains(blob, term)
}

func resourceTypeOf(v any) string {
	if env, ok := v.(*types.ResourceEnvelope); ok && env != nil {
		return env.ResourceType
	}
	obj, ok := asObject(v)
	if !ok {
		return ""
	}
	rt, _ := obj["resourceType"].(string)
	return rt
}
