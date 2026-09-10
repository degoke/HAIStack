package structuremap

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestStoreCardinalityResolverUsesStructureDefinition(t *testing.T) {
	sd := map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Patient",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Patient", "max": "1"},
				map[string]any{"path": "Patient.name", "max": "*"},
				map[string]any{"path": "Patient.name.given", "max": "*"},
				map[string]any{"path": "Patient.gender", "max": "1"},
			},
		},
	}
	raw, err := json.Marshal(sd)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &StoreCardinalityResolver{Store: memDefinitionStore{
		records: map[string][]byte{
			"http://hl7.org/fhir/StructureDefinition/Patient": raw,
		},
	}}
	repeating, ok := resolver.IsRepeating(context.Background(), "Patient.name")
	if !ok || !repeating {
		t.Fatalf("expected Patient.name to repeat, got ok=%v repeating=%v", ok, repeating)
	}
	singular, ok := resolver.IsRepeating(context.Background(), "Patient.gender")
	if !ok || singular {
		t.Fatalf("expected Patient.gender to be singular, got ok=%v repeating=%v", ok, singular)
	}
}

func TestAssignElementValueUsesStructureDefinitionCardinality(t *testing.T) {
	sd := map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation", "max": "1"},
				map[string]any{"path": "Observation.code", "max": "1"},
				map[string]any{"path": "Observation.component", "max": "*"},
			},
		},
	}
	raw, err := json.Marshal(sd)
	if err != nil {
		t.Fatal(err)
	}
	cardinality := &StoreCardinalityResolver{Store: memDefinitionStore{
		records: map[string][]byte{
			"http://hl7.org/fhir/StructureDefinition/Observation": raw,
		},
	}}
	obs := map[string]any{"resourceType": "Observation"}
	if err := assignElementValue(context.Background(), obs, []string{"code"}, map[string]any{"text": "weight"}, nil, cardinality); err != nil {
		t.Fatal(err)
	}
	if _, ok := obs["code"].([]any); ok {
		t.Fatalf("Observation.code must not be an array: %#v", obs["code"])
	}
	if err := assignElementValue(context.Background(), obs, []string{"component"}, map[string]any{"code": map[string]any{"text": "bp"}}, nil, cardinality); err != nil {
		t.Fatal(err)
	}
	components, ok := obs["component"].([]any)
	if !ok || len(components) != 1 {
		t.Fatalf("expected Observation.component array, got %#v", obs["component"])
	}
}

type memDefinitionStore struct {
	records map[string][]byte
}

func (s memDefinitionStore) Upsert(context.Context, store.DefinitionResourceRecord, []store.DefinitionTargetRecord) error {
	return nil
}

func (s memDefinitionStore) Get(_ context.Context, canonicalURL, _ string) (*store.DefinitionResourceRecord, error) {
	raw, ok := s.records[canonicalURL]
	if !ok {
		return nil, context.Canceled
	}
	return &store.DefinitionResourceRecord{CanonicalURL: canonicalURL, JSONData: raw}, nil
}

func (s memDefinitionStore) List(context.Context, store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	return nil, nil
}

func (s memDefinitionStore) Delete(context.Context, string, string) error { return nil }
