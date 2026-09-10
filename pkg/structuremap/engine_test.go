package structuremap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestEngineExecutesMinimalPatientExtraction(t *testing.T) {
	m := Map{
		URL: "http://example.org/sdc/StructureMap/example-extraction",
		Group: []Group{{
			Name: "ExtractPatient",
			Input: []Input{
				{Name: "src", Type: "QuestionnaireResponse", Mode: "source"},
				{Name: "tgt", Type: "Patient", Mode: "target"},
			},
			Rule: []Rule{{
				Name: "createPatient",
				Target: []Target{{
					Context:   "tgt",
					Transform: "create",
					Parameter: []Parameter{{ValueString: "Patient"}},
				}},
				Rule: []Rule{{
					Name: "setGiven",
					Source: []Source{{
						Context:   "src",
						Element:   []string{"item"},
						Variable:  "nameItem",
						Condition: "linkId = 'name'",
					}},
					Target: []Target{{
						Context:   "tgt",
						Element:   []string{"name"},
						Transform: "create",
						Parameter: []Parameter{{ValueString: "HumanName"}},
						Variable:  "humanName",
					}},
					Rule: []Rule{{
						Name: "copyGiven",
						Source: []Source{{
							Context:  "nameItem",
							Element:  []string{"answer"},
							Variable: "answer",
						}},
						Target: []Target{{
							Context:   "humanName",
							Element:   []string{"text"},
							Transform: "copy",
							Parameter: []Parameter{{ValueID: "answer"}},
						}},
					}},
				}},
			}},
		}},
	}
	response := map[string]any{
		"resourceType": "QuestionnaireResponse",
		"status":       "completed",
		"item": []any{
			map[string]any{
				"linkId": "name",
				"answer": []any{
					map[string]any{"valueString": "Ada"},
				},
			},
		},
	}
	resources, err := Engine{}.Execute(context.Background(), m, ExecuteInput{"src": response})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected one resource, got %d", len(resources))
	}
	if !strings.Contains(string(resources[0]), "Ada") || !strings.Contains(string(resources[0]), "Patient") {
		t.Fatalf("unexpected extraction output: %s", resources[0])
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

func TestCollectOutputsFromBundleTarget(t *testing.T) {
	bundle := map[string]any{
		"resourceType": "Bundle",
		"entry": []any{
			map[string]any{"resource": map[string]any{"resourceType": "Patient", "id": "1"}},
		},
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	resources := extractResources(bundle)
	if len(resources) != 1 || resources[0]["id"] != "1" {
		t.Fatalf("unexpected bundle extraction: %#v", resources)
	}
	_ = raw
}
