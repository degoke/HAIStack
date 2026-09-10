package structuremap

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestEngineExecutesMinimalPatientExtraction(t *testing.T) {
	resources, err := Engine{}.Execute(context.Background(), exampleExtractionMap("http://example.org/sdc/StructureMap/example-extraction"), ExecuteInput{"src": exampleResponse()})
	if err != nil {
		t.Fatal(err)
	}
	assertValidPatientExtraction(t, resources)
}

func TestCreateDatatypeOmitsResourceType(t *testing.T) {
	value, err := Engine{}.applyTransform(context.Background(), "create", []Parameter{{ValueString: "HumanName"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", value)
	}
	if _, has := object["resourceType"]; has {
		t.Fatalf("datatype should not include resourceType: %#v", object)
	}
}

func TestNestedRepeatingPathNameGiven(t *testing.T) {
	m := Map{
		URL: "http://example/map",
		Group: []Group{{
			Input: []Input{
				{Name: "src", Type: "QuestionnaireResponse", Mode: "source"},
				{Name: "tgt", Type: "Patient", Mode: "target"},
			},
			Rule: []Rule{{
				Target: []Target{{Context: "tgt", Transform: "create", Parameter: []Parameter{{ValueString: "Patient"}}}},
				Rule: []Rule{{
					Source: []Source{{Context: "src", Element: []string{"item"}, Variable: "nameItem", Condition: "linkId = 'name'"}},
					Rule: []Rule{{
						Source: []Source{{Context: "nameItem", Element: []string{"answer"}, Variable: "answer"}},
						Target: []Target{{
							Context: "tgt", Element: []string{"name", "given"}, Transform: "copy",
							Parameter: []Parameter{{ValueID: "answer"}},
						}},
					}},
				}},
			}},
		}},
	}
	resources, err := Engine{}.Execute(context.Background(), m, ExecuteInput{"src": exampleResponse()})
	if err != nil {
		t.Fatal(err)
	}
	patient := decodePatient(t, resources[0])
	name, ok := patient["name"].([]any)
	if !ok || len(name) != 1 {
		t.Fatalf("expected Patient.name array, got %#v", patient["name"])
	}
	humanName, ok := name[0].(map[string]any)
	if !ok {
		t.Fatalf("expected HumanName object, got %#v", name[0])
	}
	given, ok := humanName["given"].([]any)
	if !ok || len(given) != 1 || given[0] != "Ada" {
		t.Fatalf("expected given array with Ada, got %#v", humanName["given"])
	}
}

func TestObservationCodeIsNotArray(t *testing.T) {
	obs := map[string]any{"resourceType": "Observation"}
	if err := assignElementValue(context.Background(), obs, []string{"code"}, map[string]any{"text": "weight"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := obs["code"].([]any); ok {
		t.Fatalf("Observation.code must not be an array: %#v", obs["code"])
	}
}

func TestPatientNameIsArray(t *testing.T) {
	resources, err := Engine{}.Execute(context.Background(), exampleExtractionMap("http://example/map"), ExecuteInput{"src": exampleResponse()})
	if err != nil {
		t.Fatal(err)
	}
	patient := decodePatient(t, resources[0])
	name, ok := patient["name"].([]any)
	if !ok || len(name) != 1 {
		t.Fatalf("expected Patient.name array, got %#v", patient["name"])
	}
	humanName, ok := name[0].(map[string]any)
	if !ok {
		t.Fatalf("expected HumanName object, got %#v", name[0])
	}
	if _, has := humanName["resourceType"]; has {
		t.Fatalf("HumanName should not include resourceType: %#v", humanName)
	}
}

func TestEntryGroupSelectedByMapName(t *testing.T) {
	m := Map{
		Name: "PopulatePatient",
		URL:  "http://example/map",
		Group: []Group{
			{
				Name: "HelperOnly",
				Input: []Input{{Name: "src", Type: "QuestionnaireResponse", Mode: "source"}},
				Rule: []Rule{{
					Source: []Source{{Context: "src", Element: []string{"item"}, Variable: "item", Condition: "linkId = 'missing'"}},
					Target: []Target{{Context: "src", Element: []string{"status"}, Transform: "copy", Parameter: []Parameter{{ValueString: "failed"}}}},
				}},
			},
			{
				Name: "PopulatePatient",
				Input: []Input{
					{Name: "src", Type: "QuestionnaireResponse", Mode: "source"},
					{Name: "tgt", Type: "Patient", Mode: "target"},
				},
				Rule: []Rule{{
					Target: []Target{{
						Context: "tgt", Transform: "create", Parameter: []Parameter{{ValueString: "Patient"}},
					}},
				}},
			},
		},
	}
	resources, err := Engine{}.Execute(context.Background(), m, ExecuteInput{"src": exampleResponse()})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || decodePatient(t, resources[0])["resourceType"] != "Patient" {
		t.Fatalf("expected patient from named entry group, got %#v", resources)
	}
}

func TestStrictModeFailsOnUnmatchedSource(t *testing.T) {
	m := Map{
		URL: "http://example/map",
		Group: []Group{{
			Name: "Extract",
			Input: []Input{
				{Name: "src", Type: "QuestionnaireResponse", Mode: "source"},
				{Name: "tgt", Type: "Patient", Mode: "target"},
			},
			Rule: []Rule{{
				Name: "missing",
				Source: []Source{{
					Context: "src", Element: []string{"item"}, Variable: "item", Condition: "linkId = 'missing'",
				}},
				Target: []Target{{
					Context: "tgt", Transform: "create", Parameter: []Parameter{{ValueString: "Patient"}},
				}},
			}},
		}},
	}
	_, err := Engine{Strict: true}.Execute(context.Background(), m, ExecuteInput{"src": exampleResponse()})
	if err == nil || !strings.Contains(err.Error(), "no source bindings matched") {
		t.Fatalf("expected strict unmatched source error, got %v", err)
	}
}

func TestDependentGroupInvocation(t *testing.T) {
	m := Map{
		Name: "ExtractPatient",
		URL:  "http://example/map",
		Group: []Group{
			{
				Name: "ExtractPatient",
				Input: []Input{
					{Name: "src", Type: "QuestionnaireResponse", Mode: "source"},
					{Name: "tgt", Type: "Patient", Mode: "target"},
				},
				Rule: []Rule{{
					Target: []Target{{
						Context: "tgt", Transform: "create", Parameter: []Parameter{{ValueString: "Patient"}},
					}},
					Dependent: []Dependent{{Name: "PopulateName", Variable: []string{"src", "tgt"}}},
				}},
			},
			{
				Name: "PopulateName",
				Input: []Input{
					{Name: "src", Type: "QuestionnaireResponse", Mode: "source"},
					{Name: "tgt", Type: "Patient", Mode: "target"},
				},
				Rule: []Rule{{
					Source: []Source{{
						Context: "src", Element: []string{"item"}, Variable: "nameItem", Condition: "linkId = 'name'",
					}},
					Target: []Target{{
						Context: "tgt", Element: []string{"name"}, Transform: "create",
						Parameter: []Parameter{{ValueString: "HumanName"}}, Variable: "humanName",
					}},
					Rule: []Rule{{
						Source: []Source{{Context: "nameItem", Element: []string{"answer"}, Variable: "answer"}},
						Target: []Target{{
							Context: "humanName", Element: []string{"text"}, Transform: "copy",
							Parameter: []Parameter{{ValueID: "answer"}},
						}},
					}},
				}},
			},
		},
	}
	resources, err := Engine{}.Execute(context.Background(), m, ExecuteInput{"src": exampleResponse()})
	if err != nil {
		t.Fatal(err)
	}
	assertValidPatientExtraction(t, resources)
}

func TestBundleTargetOutputExtraction(t *testing.T) {
	m := Map{
		Name: "ExtractBundle",
		URL:  "http://example/map",
		Group: []Group{{
			Name: "ExtractBundle",
			Input: []Input{
				{Name: "src", Type: "QuestionnaireResponse", Mode: "source"},
				{Name: "bundle", Type: "Bundle", Mode: "target"},
			},
			Rule: []Rule{{
				Target: []Target{{
					Context: "bundle", Transform: "create", Parameter: []Parameter{{ValueString: "Bundle"}},
				}},
				Rule: []Rule{{
					Target: []Target{{
						Context: "bundle", Element: []string{"entry"}, Transform: "create",
						Parameter: []Parameter{{ValueString: "BundleEntry"}}, Variable: "entry",
					}},
					Rule: []Rule{{
						Target: []Target{{
							Context: "entry", Element: []string{"resource"}, Transform: "create",
							Parameter: []Parameter{{ValueString: "Patient"}}, Variable: "patient",
						}},
						Rule: []Rule{{
							Source: []Source{{Context: "src", Element: []string{"item"}, Variable: "nameItem", Condition: "linkId = 'name'"}},
							Target: []Target{{
								Context: "patient", Element: []string{"name"}, Transform: "create",
								Parameter: []Parameter{{ValueString: "HumanName"}}, Variable: "humanName",
							}},
							Rule: []Rule{{
								Source: []Source{{Context: "nameItem", Element: []string{"answer"}, Variable: "answer"}},
								Target: []Target{{
									Context: "humanName", Element: []string{"text"}, Transform: "copy",
									Parameter: []Parameter{{ValueID: "answer"}},
								}},
							}},
						}},
					}},
				}},
			}},
		}},
	}
	resources, err := Engine{}.Execute(context.Background(), m, ExecuteInput{"src": exampleResponse()})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected one extracted bundle resource, got %d", len(resources))
	}
	assertValidPatientExtraction(t, resources)
}

func TestExampleExtractionTemplateArtifact(t *testing.T) {
	raw, err := os.ReadFile("../../modules/sdc/examples/extraction-template.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMap(raw)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := Engine{}.Execute(context.Background(), m, ExecuteInput{"src": exampleResponse()})
	if err != nil {
		t.Fatal(err)
	}
	assertValidPatientExtraction(t, resources)
}

func TestStoreResolverUsesResourceStore(t *testing.T) {
	mapURL := "http://example.org/sdc/StructureMap/example-extraction"
	raw, err := json.Marshal(exampleExtractionMap(mapURL))
	if err != nil {
		t.Fatal(err)
	}
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"StructureMap": {
			"map-1": {ResourceType: "StructureMap", ID: "map-1", JSON: raw},
		},
	}}
	resolver := &StoreResolver{Resources: store}
	m, err := resolver.Resolve(context.Background(), mapURL)
	if err != nil {
		t.Fatal(err)
	}
	if m.URL != mapURL {
		t.Fatalf("unexpected map: %#v", m)
	}
	m2, err := resolver.Resolve(context.Background(), mapURL)
	if err != nil {
		t.Fatal(err)
	}
	if m2.URL != mapURL {
		t.Fatalf("cached resolve failed: %#v", m2)
	}
}

func TestEngineHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Engine{}.Execute(ctx, exampleExtractionMap("http://example/map"), ExecuteInput{"src": exampleResponse()})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestSourceListModeFirst(t *testing.T) {
	m := Map{
		URL: "http://example/map",
		Group: []Group{{
			Input: []Input{
				{Name: "src", Type: "QuestionnaireResponse", Mode: "source"},
				{Name: "tgt", Type: "Patient", Mode: "target"},
			},
			Rule: []Rule{{
				Target: []Target{{Context: "tgt", Transform: "create", Parameter: []Parameter{{ValueString: "Patient"}}}},
				Rule: []Rule{{
					Source: []Source{{
						Context: "src", Element: []string{"item"}, Variable: "item", ListMode: "first",
					}},
					Target: []Target{{
						Context: "tgt", Element: []string{"identifier"}, Transform: "create",
						Parameter: []Parameter{{ValueString: "Identifier"}}, Variable: "id",
					}},
					Rule: []Rule{{
						Source: []Source{{Context: "item", Element: []string{"linkId"}, Variable: "linkId"}},
						Target: []Target{{
							Context: "id", Element: []string{"value"}, Transform: "copy",
							Parameter: []Parameter{{ValueID: "linkId"}},
						}},
					}},
				}},
			}},
		}},
	}
	response := map[string]any{
		"resourceType": "QuestionnaireResponse",
		"item": []any{
			map[string]any{"linkId": "first"},
			map[string]any{"linkId": "second"},
		},
	}
	resources, err := Engine{}.Execute(context.Background(), m, ExecuteInput{"src": response})
	if err != nil {
		t.Fatal(err)
	}
	patient := decodePatient(t, resources[0])
	identifiers, ok := patient["identifier"].([]any)
	if !ok || len(identifiers) != 1 {
		t.Fatalf("expected one identifier, got %#v", patient["identifier"])
	}
	id := identifiers[0].(map[string]any)
	if id["value"] != "first" {
		t.Fatalf("expected first item linkId, got %#v", id)
	}
}

func TestTransformsReferenceAndAppend(t *testing.T) {
	ref, err := Engine{}.applyTransform(context.Background(), "reference", []Parameter{{ValueString: "Patient"}, {ValueString: "1"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ref.(map[string]any)["reference"] != "Patient/1" {
		t.Fatalf("unexpected reference: %#v", ref)
	}
	appended, err := Engine{}.applyTransform(context.Background(), "append", []Parameter{{ValueString: "urn:uuid:"}, {ValueString: "abc"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if appended != "urn:uuid:abc" {
		t.Fatalf("unexpected append result: %v", appended)
	}
}

func TestStoreResolverMissingMap(t *testing.T) {
	_, err := StaticResolver{}.Resolve(context.Background(), "http://example.org/missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing map error, got %v", err)
	}
}

func TestParseMap(t *testing.T) {
	raw := []byte(`{"resourceType":"StructureMap","url":"http://example/map","group":[{"name":"g"}]}`)
	m, err := ParseMap(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.URL != "http://example/map" || len(m.Group) != 1 {
		t.Fatalf("unexpected map: %#v", m)
	}
}

func exampleResponse() map[string]any {
	return map[string]any{
		"resourceType": "QuestionnaireResponse",
		"status":       "completed",
		"item": []any{
			map[string]any{
				"linkId": "name",
				"answer": []any{map[string]any{"valueString": "Ada"}},
			},
		},
	}
}

func assertValidPatientExtraction(t *testing.T, resources []json.RawMessage) {
	if len(resources) != 1 {
		t.Fatalf("expected one resource, got %d", len(resources))
	}
	patient := decodePatient(t, resources[0])
	if patient["resourceType"] != "Patient" {
		t.Fatalf("expected Patient resource, got %#v", patient)
	}
	name, ok := patient["name"].([]any)
	if !ok || len(name) != 1 {
		t.Fatalf("expected Patient.name array, got %#v", patient["name"])
	}
	humanName, ok := name[0].(map[string]any)
	if !ok {
		t.Fatalf("expected HumanName object, got %#v", name[0])
	}
	if humanName["text"] != "Ada" {
		t.Fatalf("expected extracted name Ada, got %#v", humanName)
	}
	if _, has := humanName["resourceType"]; has {
		t.Fatalf("HumanName must not include resourceType")
	}
}

func decodePatient(t *testing.T, raw json.RawMessage) map[string]any {
	var patient map[string]any
	if err := json.Unmarshal(raw, &patient); err != nil {
		t.Fatal(err)
	}
	return patient
}

type testResourceStore struct {
	byType map[string]map[string]*types.ResourceEnvelope
}

func (s *testResourceStore) Create(context.Context, *types.ResourceEnvelope) error { return nil }
func (s *testResourceStore) Update(context.Context, *types.ResourceEnvelope) error { return nil }
func (s *testResourceStore) Delete(context.Context, string, string) error          { return nil }
func (s *testResourceStore) Exists(_ context.Context, resourceType, id string) (bool, error) {
	_, ok := s.byType[resourceType][id]
	return ok, nil
}
func (s *testResourceStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	if env, ok := s.byType[resourceType][id]; ok {
		return env, nil
	}
	return nil, context.Canceled
}
func (s *testResourceStore) ListIDs(_ context.Context, resourceType string, _, _ int) ([]string, error) {
	byID := s.byType[resourceType]
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	return ids, nil
}
