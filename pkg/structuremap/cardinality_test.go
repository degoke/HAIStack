package structuremap

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/registry"
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
	cardinality := newMapCardinalityResolver(memDefinitionStore{
		records: map[string][]byte{
			"http://hl7.org/fhir/StructureDefinition/Observation": raw,
		},
	}, Map{})
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

func TestMapCardinalityResolverUsesProfileStructure(t *testing.T) {
	baseSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation", "max": "1"},
				map[string]any{"path": "Observation.component", "max": "*"},
			},
		},
	})
	profileSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/single-component-observation",
		"type":         "Observation",
		"derivation":   "constraint",
		"differential": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.component", "max": "1"},
			},
		},
	})
	store := memDefinitionStore{records: map[string][]byte{
		"http://hl7.org/fhir/StructureDefinition/Observation":                 baseSD,
		"http://example.org/StructureDefinition/single-component-observation": profileSD,
	}}
	resolver := newMapCardinalityResolver(store, Map{
		Structure: []Structure{{
			URL:  "http://example.org/StructureDefinition/single-component-observation",
			Mode: "target",
		}},
	})
	obs := map[string]any{"resourceType": "Observation"}
	repeating, ok := resolver.IsRepeatingFor(context.Background(), obs, "Observation.component")
	if !ok || repeating {
		t.Fatalf("expected profile-constrained singular component, got ok=%v repeating=%v", ok, repeating)
	}
}

func TestMapCardinalityResolverUsesMetaProfile(t *testing.T) {
	baseSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.component", "max": "*"},
			},
		},
	})
	profileSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/single-component-observation",
		"type":         "Observation",
		"derivation":   "constraint",
		"differential": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.component", "max": "1"},
			},
		},
	})
	store := memDefinitionStore{records: map[string][]byte{
		"http://hl7.org/fhir/StructureDefinition/Observation":                 baseSD,
		"http://example.org/StructureDefinition/single-component-observation": profileSD,
	}}
	resolver := newMapCardinalityResolver(store, Map{})
	obs := map[string]any{
		"resourceType": "Observation",
		"meta": map[string]any{
			"profile": []any{"http://example.org/StructureDefinition/single-component-observation"},
		},
	}
	repeating, ok := resolver.IsRepeatingFor(context.Background(), obs, "Observation.component")
	if !ok || repeating {
		t.Fatalf("expected meta.profile singular component, got ok=%v repeating=%v", ok, repeating)
	}
}

func TestMapCardinalityResolverCacheIsPerExecution(t *testing.T) {
	store := testDefinitionStore()
	first := newMapCardinalityResolver(store, Map{})
	second := newMapCardinalityResolver(store, Map{})
	if first == second {
		t.Fatal("expected distinct per-map resolver instances")
	}
}

func testDefinitionStore() memDefinitionStore {
	patientSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Patient",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Patient", "max": "1"},
				map[string]any{"path": "Patient.name", "max": "*"},
				map[string]any{"path": "Patient.name.given", "max": "*"},
				map[string]any{"path": "Patient.identifier", "max": "*"},
				map[string]any{"path": "Patient.gender", "max": "1"},
			},
		},
	})
	observationSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation", "max": "1"},
				map[string]any{"path": "Observation.code", "max": "1"},
				map[string]any{"path": "Observation.component", "max": "*"},
			},
		},
	})
	bundleSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Bundle",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Bundle", "max": "1"},
				map[string]any{"path": "Bundle.entry", "max": "*"},
			},
		},
	})
	return memDefinitionStore{records: map[string][]byte{
		"http://hl7.org/fhir/StructureDefinition/Patient":      patientSD,
		"http://hl7.org/fhir/StructureDefinition/Observation": observationSD,
		"http://hl7.org/fhir/StructureDefinition/Bundle":      bundleSD,
	}}
}

func testEngine() Engine {
	return Engine{Cardinality: &StoreCardinalityResolver{Store: registry.EmbeddedDefinitionStore()}}
}

func TestCardinalityUsesHL7ObservationValueQuantityChoice(t *testing.T) {
	resolver := newMapCardinalityResolver(registry.EmbeddedDefinitionStore(), Map{})
	singular, ok := resolver.IsRepeating(context.Background(), "Observation.valueQuantity")
	if !ok || singular {
		t.Fatalf("expected HL7 Observation.valueQuantity to be singular, got ok=%v repeating=%v", ok, singular)
	}
}

func TestCardinalityUsesHL7PatientStructureDefinition(t *testing.T) {
	resolver := newMapCardinalityResolver(registry.EmbeddedDefinitionStore(), Map{})
	repeating, ok := resolver.IsRepeating(context.Background(), "Patient.name")
	if !ok || !repeating {
		t.Fatalf("expected HL7 Patient.name to repeat, got ok=%v repeating=%v", ok, repeating)
	}
	singular, ok := resolver.IsRepeating(context.Background(), "Patient.gender")
	if !ok || singular {
		t.Fatalf("expected HL7 Patient.gender to be singular, got ok=%v repeating=%v", ok, singular)
	}
}

func TestEngineWithoutCardinalityStoreUsesEmbeddedR4(t *testing.T) {
	resources, err := Engine{}.Execute(context.Background(), exampleExtractionMap("http://example/map"), ExecuteInput{"src": exampleResponse()})
	if err != nil {
		t.Fatal(err)
	}
	assertValidPatientExtraction(t, resources)
}

func TestMapCardinalityResolverMergesProfileWithBase(t *testing.T) {
	baseSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Observation",
		"type":         "Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.code", "max": "1"},
				map[string]any{"path": "Observation.component", "max": "*"},
			},
		},
	})
	profileSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/obs-profile",
		"type":         "Observation",
		"derivation":   "constraint",
		"differential": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.component", "max": "1"},
			},
		},
	})
	store := memDefinitionStore{records: map[string][]byte{
		"http://hl7.org/fhir/StructureDefinition/Observation": baseSD,
		"http://example.org/StructureDefinition/obs-profile":  profileSD,
	}}
	resolver := newMapCardinalityResolver(store, Map{})
	index, err := resolver.loadIndex(context.Background(), "http://example.org/StructureDefinition/obs-profile", "Observation")
	if err != nil {
		t.Fatal(err)
	}
	if index["Observation.component"] != "1" {
		t.Fatalf("expected profile override for component, got %#v", index["Observation.component"])
	}
	if index["Observation.code"] != "1" {
		t.Fatalf("expected merged base path Observation.code, got %#v", index["Observation.code"])
	}
}

func TestMapCardinalityResolverFirstRepeatingProfileWins(t *testing.T) {
	firstRepeating, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/first-repeating",
		"type":         "Observation",
		"derivation":   "constraint",
		"differential": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.component", "max": "2"},
			},
		},
	})
	secondRepeating, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/second-repeating",
		"type":         "Observation",
		"derivation":   "constraint",
		"differential": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.component", "max": "*"},
			},
		},
	})
	baseSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.component", "max": "1"},
			},
		},
	})
	store := memDefinitionStore{records: map[string][]byte{
		"http://hl7.org/fhir/StructureDefinition/Observation":        baseSD,
		"http://example.org/StructureDefinition/first-repeating":    firstRepeating,
		"http://example.org/StructureDefinition/second-repeating":   secondRepeating,
	}}
	resolver := newMapCardinalityResolver(store, Map{})
	obs := map[string]any{
		"resourceType": "Observation",
		"meta": map[string]any{
			"profile": []any{
				"http://example.org/StructureDefinition/first-repeating",
				"http://example.org/StructureDefinition/second-repeating",
			},
		},
	}
	repeating, ok := resolver.IsRepeatingFor(context.Background(), obs, "Observation.component")
	if !ok || !repeating {
		t.Fatalf("expected first repeating profile to win over base singular, got ok=%v repeating=%v", ok, repeating)
	}
}

func TestMapCardinalityResolverSnapshotProfileDoesNotFallBackToBase(t *testing.T) {
	baseSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Observation",
		"type":         "Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.code", "max": "1"},
				map[string]any{"path": "Observation.component", "max": "*"},
			},
		},
	})
	profileSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/snapshot-only",
		"type":         "Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation", "max": "1"},
				map[string]any{"path": "Observation.component", "max": "1"},
			},
		},
	})
	store := memDefinitionStore{records: map[string][]byte{
		"http://hl7.org/fhir/StructureDefinition/Observation":  baseSD,
		"http://example.org/StructureDefinition/snapshot-only": profileSD,
	}}
	resolver := newMapCardinalityResolver(store, Map{})
	obs := map[string]any{
		"resourceType": "Observation",
		"meta": map[string]any{
			"profile": []any{"http://example.org/StructureDefinition/snapshot-only"},
		},
	}
	repeating, ok := resolver.IsRepeatingFor(context.Background(), obs, "Observation.code")
	if ok {
		t.Fatalf("snapshot-authoritative profile must not inherit base path cardinality, got repeating=%v", repeating)
	}
}

func TestMapCardinalityResolverConstraintWithSnapshotUsesSnapshotElements(t *testing.T) {
	profileSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/constraint-with-snapshot",
		"type":         "Observation",
		"derivation":   "constraint",
		"differential": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.component", "max": "*"},
			},
		},
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation", "max": "1"},
				map[string]any{"path": "Observation.component", "max": "1"},
			},
		},
	})
	store := memDefinitionStore{records: map[string][]byte{
		"http://example.org/StructureDefinition/constraint-with-snapshot": profileSD,
	}}
	resolver := newMapCardinalityResolver(store, Map{})
	index, err := resolver.loadIndex(context.Background(), "http://example.org/StructureDefinition/constraint-with-snapshot", "Observation")
	if err != nil {
		t.Fatal(err)
	}
	if index["Observation.component"] != "1" {
		t.Fatalf("expected snapshot component max, got %#v", index["Observation.component"])
	}
}

func TestMapCardinalityResolverSnapshotProfileDoesNotMergeBase(t *testing.T) {
	baseSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Observation",
		"type":         "Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.code", "max": "1"},
				map[string]any{"path": "Observation.component", "max": "*"},
			},
		},
	})
	profileSD, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/snapshot-only",
		"type":         "Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation", "max": "1"},
				map[string]any{"path": "Observation.component", "max": "1"},
			},
		},
	})
	store := memDefinitionStore{records: map[string][]byte{
		"http://hl7.org/fhir/StructureDefinition/Observation": baseSD,
		"http://example.org/StructureDefinition/snapshot-only": profileSD,
	}}
	resolver := newMapCardinalityResolver(store, Map{})
	index, err := resolver.loadIndex(context.Background(), "http://example.org/StructureDefinition/snapshot-only", "Observation")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := index["Observation.code"]; ok {
		t.Fatalf("snapshot-only profile must not inherit base paths: %#v", index)
	}
	if index["Observation.component"] != "1" {
		t.Fatalf("expected snapshot component max 1, got %#v", index["Observation.component"])
	}
}

func TestMapCardinalityResolverIndexesSliceNames(t *testing.T) {
	sd := map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Patient",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{"path": "Patient.identifier", "max": "*"},
				map[string]any{"path": "Patient.identifier", "sliceName": "usual", "max": "1"},
			},
		},
	}
	raw, err := json.Marshal(sd)
	if err != nil {
		t.Fatal(err)
	}
	resolver := newMapCardinalityResolver(memDefinitionStore{
		records: map[string][]byte{
			"http://hl7.org/fhir/StructureDefinition/Patient": raw,
		},
	}, Map{})
	repeating, ok := resolver.IsRepeating(context.Background(), "Patient.identifier")
	if !ok || !repeating {
		t.Fatalf("expected Patient.identifier to repeat, got ok=%v repeating=%v", ok, repeating)
	}
	singular, ok := resolver.IsRepeating(context.Background(), "Patient.identifier:usual")
	if !ok || singular {
		t.Fatalf("expected Patient.identifier:usual to be singular, got ok=%v repeating=%v", ok, singular)
	}
}

func TestMapCardinalityResolverIndexesChoiceTypes(t *testing.T) {
	sd := map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://hl7.org/fhir/StructureDefinition/Observation",
		"snapshot": map[string]any{
			"element": []any{
				map[string]any{
					"path": "Observation.value[x]",
					"max":  "1",
					"type": []any{
						map[string]any{"code": "Quantity"},
						map[string]any{"code": "string"},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(sd)
	if err != nil {
		t.Fatal(err)
	}
	resolver := newMapCardinalityResolver(memDefinitionStore{
		records: map[string][]byte{
			"http://hl7.org/fhir/StructureDefinition/Observation": raw,
		},
	}, Map{})
	singular, ok := resolver.IsRepeating(context.Background(), "Observation.valueQuantity")
	if !ok || singular {
		t.Fatalf("expected Observation.valueQuantity to be singular, got ok=%v repeating=%v", ok, singular)
	}
}

func TestMapCardinalityResolverPrefersSingularProfile(t *testing.T) {
	repeatingProfile, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/repeating-profile",
		"type":         "Observation",
		"derivation":   "constraint",
		"differential": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.component", "max": "*"},
			},
		},
	})
	singularProfile, _ := json.Marshal(map[string]any{
		"resourceType": "StructureDefinition",
		"url":          "http://example.org/StructureDefinition/singular-profile",
		"type":         "Observation",
		"derivation":   "constraint",
		"differential": map[string]any{
			"element": []any{
				map[string]any{"path": "Observation.component", "max": "1"},
			},
		},
	})
	store := memDefinitionStore{records: map[string][]byte{
		"http://example.org/StructureDefinition/repeating-profile": repeatingProfile,
		"http://example.org/StructureDefinition/singular-profile":  singularProfile,
	}}
	resolver := newMapCardinalityResolver(store, Map{})
	obs := map[string]any{
		"resourceType": "Observation",
		"meta": map[string]any{
			"profile": []any{
				"http://example.org/StructureDefinition/repeating-profile",
				"http://example.org/StructureDefinition/singular-profile",
			},
		},
	}
	repeating, ok := resolver.IsRepeatingFor(context.Background(), obs, "Observation.component")
	if !ok || repeating {
		t.Fatalf("expected singular profile to win across meta.profile, got ok=%v repeating=%v", ok, repeating)
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
