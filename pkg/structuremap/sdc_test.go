package structuremap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/sdc"
)

func TestQuestionnaireExtractorUsesStructureMapEngine(t *testing.T) {
	mapURL := "http://example.org/sdc/StructureMap/example-extraction"
	extractor := NewExtractor(Config{
		Resolver: StaticResolver{mapURL: exampleExtractionMap(mapURL)},
		Engine:   testEngine(),
	})
	q := sdc.Questionnaire{
		ResourceType:       "Questionnaire",
		URL:                "http://example/q",
		Status:             "active",
		SourceStructureMap: mapURL,
		Item:               []sdc.Item{{LinkID: "name", Type: "string"}},
	}
	r := sdc.QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
		Item:         []sdc.ResponseItem{{LinkID: "name", Answer: []sdc.Answer{{Value: "Ada"}}}},
	}
	result, err := sdc.QuestionnaireExtractor{StructureMap: extractor}.Extract(context.Background(), q, r)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bundle == nil {
		t.Fatal("expected bundle")
	}
	if !strings.Contains(string(result.Bundle.JSON), "Ada") {
		t.Fatalf("expected extracted patient name: %s", result.Bundle.JSON)
	}
	foundStructureMapDiagnostic := false
	for _, diagnostic := range result.Diagnostics {
		if strings.Contains(diagnostic.Message, "sourceStructureMap") || strings.Contains(diagnostic.Message, "StructureMap") {
			foundStructureMapDiagnostic = true
		}
	}
	if !foundStructureMapDiagnostic {
		t.Fatalf("expected StructureMap diagnostics: %#v", result.Diagnostics)
	}
}

func TestQuestionnaireExtractorReportsMissingMap(t *testing.T) {
	extractor := NewExtractor(Config{
		Resolver: StaticResolver{},
		Engine:   testEngine(),
	})
	q := sdc.Questionnaire{
		ResourceType:       "Questionnaire",
		URL:                "http://example/q",
		Status:             "active",
		SourceStructureMap: "http://example.org/missing",
	}
	_, err := sdc.QuestionnaireExtractor{StructureMap: extractor}.Extract(context.Background(), q, sdc.QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "in-progress",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing map error, got %v", err)
	}
}

func exampleExtractionMap(url string) Map {
	return Map{
		URL: url,
		Group: []Group{{
			Name: "ExtractPatient",
			Input: []Input{
				{Name: "src", Type: "QuestionnaireResponse", Mode: "source"},
				{Name: "tgt", Type: "Patient", Mode: "target"},
			},
			Rule: []Rule{{
				Target: []Target{{
					Context:   "tgt",
					Transform: "create",
					Parameter: []Parameter{{ValueString: "Patient"}},
				}},
				Rule: []Rule{{
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
}

func TestExtractorRunProducesJSONResources(t *testing.T) {
	run := ExtractorRun(Config{
		Resolver: StaticResolver{"http://example/map": exampleExtractionMap("http://example/map")},
		Engine:   testEngine(),
	})
	raw, err := run(context.Background(), sdc.Questionnaire{
		ResourceType:       "Questionnaire",
		URL:                "http://example/q",
		SourceStructureMap: "http://example/map",
	}, sdc.QuestionnaireResponse{
		ResourceType: "QuestionnaireResponse",
		Status:       "completed",
		Item:         []sdc.ResponseItem{{LinkID: "name", Answer: []sdc.Answer{{Value: "Ada"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 {
		t.Fatalf("expected one resource, got %d", len(raw))
	}
	var patient map[string]any
	if err := json.Unmarshal(raw[0], &patient); err != nil {
		t.Fatal(err)
	}
	if patient["resourceType"] != "Patient" {
		t.Fatalf("unexpected resource: %#v", patient)
	}
	name, ok := patient["name"].([]any)
	if !ok || len(name) != 1 {
		t.Fatalf("expected Patient.name array, got %#v", patient["name"])
	}
}
