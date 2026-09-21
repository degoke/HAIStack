package cql

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/sdc"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func demoELMJSON() string {
	return `{
  "library": {
    "identifier": {"id": "Demo", "version": "1.0.0"},
    "schemaIdentifier": {"id": "urn:hl7-org:elm", "version": "r1"},
    "usings": {
      "def": [
        {"localIdentifier": "System", "uri": "urn:hl7-org:elm-types:r1"},
        {"localIdentifier": "FHIR", "uri": "http://hl7.org/fhir", "version": "4.0.1"}
      ]
    },
    "includes": {
      "def": [{"localIdentifier": "FHIRHelpers", "path": "FHIRHelpers", "version": "4.0.1"}]
    },
    "parameters": {
      "def": [{
        "name": "Threshold",
        "parameterTypeSpecifier": {"name": "{urn:hl7-org:elm-types:r1}Integer"},
        "default": {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "18"}
      }]
    },
    "statements": {
      "def": [
        {
          "name": "Patient",
          "context": "Patient",
          "expression": {
            "type": "SingletonFrom",
            "operand": {"type": "Retrieve", "dataType": "{http://hl7.org/fhir}Patient"}
          }
        },
        {
          "name": "X",
          "context": "Patient",
          "accessLevel": "Public",
          "expression": {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Boolean", "value": "true"}
        },
        {
          "name": "Given",
          "context": "Patient",
          "accessLevel": "Public",
          "expression": {
            "type": "First",
            "source": {
              "type": "Property",
              "path": "given",
              "source": {
                "type": "Property",
                "path": "name",
                "source": {"type": "ExpressionRef", "name": "Patient"}
              }
            }
          }
        },
        {
          "name": "Adult",
          "context": "Patient",
          "accessLevel": "Public",
          "expression": {
            "type": "GreaterOrEqual",
            "operand": [
              {"type": "CalculateAge", "precision": "Year", "operand": {"type": "Property", "path": "birthDate", "source": {"type": "ExpressionRef", "name": "Patient"}}},
              {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "18"}
            ]
          }
        },
        {
          "type": "FunctionDef",
          "name": "Plus",
          "accessLevel": "Public",
          "operand": [
            {"name": "a", "operandTypeSpecifier": {"name": "{urn:hl7-org:elm-types:r1}Integer"}},
            {"name": "b", "operandTypeSpecifier": {"name": "{urn:hl7-org:elm-types:r1}Integer"}}
          ],
          "expression": {
            "type": "Add",
            "operand": [
              {"type": "OperandRef", "name": "a"},
              {"type": "OperandRef", "name": "b"}
            ]
          }
        },
        {
          "name": "Sum",
          "context": "Patient",
          "expression": {
            "type": "FunctionRef",
            "name": "Plus",
            "operand": [
              {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"},
              {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "2"}
            ]
          }
        },
        {
          "name": "FinalObs",
          "context": "Patient",
          "expression": {
            "type": "Query",
            "source": [{
              "alias": "O",
              "expression": {"type": "Retrieve", "dataType": "{http://hl7.org/fhir}Observation"}
            }],
            "where": {
              "type": "Equal",
              "operand": [
                {"type": "Property", "path": "status", "scope": "O"},
                {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}String", "value": "final"}
              ]
            },
            "return": {
              "distinct": false,
              "expression": {"type": "Property", "path": "id", "scope": "O"}
            }
          }
        }
      ]
    }
  }
}`
}

func elmLibraryEnvelope(t *testing.T, elm string) *types.ResourceEnvelope {
	t.Helper()
	env, err := types.NewJSONCodec().ParseJSON("Library", []byte(`{
		"resourceType": "Library",
		"id": "demo",
		"url": "http://example.org/Library/Demo",
		"name": "Demo",
		"version": "1.0.0",
		"status": "active",
		"content": [{"contentType": "application/elm+json", "data": "`+base64.StdEncoding.EncodeToString([]byte(elm))+`"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestParseELMLibraryEvaluatesDefines(t *testing.T) {
	eng := testEngine(t)
	lib, err := eng.ParseELM([]byte(demoELMJSON()))
	if err != nil {
		t.Fatal(err)
	}
	if lib.Name != "Demo" || lib.Version != "1.0.0" || lib.Using != "FHIR" {
		t.Fatalf("meta: %+v", lib)
	}
	if len(lib.Includes) != 1 || lib.Includes[0].Name != "FHIRHelpers" {
		t.Fatalf("includes: %#v", lib.Includes)
	}
	env := EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}}
	got, err := eng.EvalDefine(context.Background(), lib, "X", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("X: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "Given", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "Ada" {
		t.Fatalf("Given: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "Adult", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("Adult: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "Sum", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(3) {
		t.Fatalf("Sum: %#v", got)
	}
}

func TestCompileELMLibraryResource(t *testing.T) {
	eng := testEngine(t)
	env := elmLibraryEnvelope(t, demoELMJSON())
	_, _, _, _, err := EnvelopeLibrary(env)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("EnvelopeLibrary should still require CQL source, got %v", err)
	}
	lib, err := eng.CompileLibrary(env)
	if err != nil {
		t.Fatal(err)
	}
	if lib.URL != "http://example.org/Library/Demo" || lib.Name != "Demo" {
		t.Fatalf("compiled meta: %+v", lib)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "X", EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("compiled X: %#v", got)
	}
}

func TestCompileELMQueryRetrieve(t *testing.T) {
	obs, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation",
		"id": "hr",
		"status": "final",
		"code": {"text": "Heart rate"},
		"subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{Retriever: StaticRetriever{obs}})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := eng.ParseELM([]byte(demoELMJSON()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "FinalObs", EvalContext{
		Patient:   adaPatient(t),
		Libraries: []*Library{lib},
		Retriever: StaticRetriever{obs},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "hr" {
		t.Fatalf("query: %#v", got)
	}
}

func TestRecoverCQLFromELMPayload(t *testing.T) {
	cqlSrc := "library Recovered version '1.0.0'\ncontext Patient\ndefine \"X\": true\n"
	payload, err := json.Marshal(map[string]any{
		"library": map[string]any{
			"identifier": map[string]any{"id": "Recovered", "version": "1.0.0"},
			"annotation": []any{
				map[string]any{"type": "CqlToElmInfo"},
			},
			"cql": cqlSrc,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	env := elmLibraryEnvelope(t, string(payload))
	src, _, name, _, err := EnvelopeLibrary(env)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Demo" {
		t.Fatalf("envelope name: %s", name)
	}
	if !strings.Contains(src, `define "X"`) {
		t.Fatalf("recovered source: %s", src)
	}
	eng := testEngine(t)
	lib, err := eng.CompileLibrary(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "X", EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("recovered eval: %#v", got)
	}
}

func TestRecoverCQLFromELMAnnotation(t *testing.T) {
	payload := `{
		"library": {
			"identifier": {"id": "Ann", "version": "1.0.0"},
			"annotation": [{
				"type": "Annotation",
				"s": {
					"s": [
						{"value": ["library Ann version '1.0.0'\n"]},
						{"value": ["context Patient\n"]},
						{"value": ["define \"X\": true\n"]}
					]
				}
			}],
			"statements": {"def": []}
		}
	}`
	env := elmLibraryEnvelope(t, payload)
	src, _, _, _, err := EnvelopeLibrary(env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "library Ann") || !strings.Contains(src, `define "X"`) {
		t.Fatalf("annotation source: %s", src)
	}
}

func TestAttachLibraryCQL(t *testing.T) {
	env := elmLibraryEnvelope(t, demoELMJSON())
	src := "library Demo version '1.0.0'\ncontext Patient\ndefine \"X\": false\n"
	attached, err := AttachLibraryCQL(env, src)
	if err != nil {
		t.Fatal(err)
	}
	got, url, name, version, err := EnvelopeLibrary(attached)
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://example.org/Library/Demo" || name != "Demo" || version != "1.0.0" {
		t.Fatalf("meta: %s %s %s", url, name, version)
	}
	if !strings.Contains(got, `define "X": false`) {
		t.Fatalf("attached source: %s", got)
	}
	eng := testEngine(t)
	lib, err := eng.CompileLibrary(attached)
	if err != nil {
		t.Fatal(err)
	}
	vals, err := eng.EvalDefine(context.Background(), lib, "X", EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != 1 || vals[0] != false {
		t.Fatalf("attached eval prefers CQL: %#v", vals)
	}
}

func TestStoreLibraryResolverCompilesELM(t *testing.T) {
	env := elmLibraryEnvelope(t, demoELMJSON())
	store := &testResourceStore{byType: map[string]map[string]*types.ResourceEnvelope{
		"Library": {"demo": env},
	}}
	eng := testEngine(t)
	r := &StoreLibraryResolver{Resources: store, Engine: eng}
	lib, err := r.Resolve(context.Background(), "http://example.org/Library/Demo")
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "Given", EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "Ada" {
		t.Fatalf("resolved ELM: %#v", got)
	}
}

func TestSDCContainedELMLibrary(t *testing.T) {
	eng := testEngine(t)
	provider := NewProvider(eng, nil)
	var elmObj map[string]any
	if err := json.Unmarshal([]byte(demoELMJSON()), &elmObj); err != nil {
		t.Fatal(err)
	}
	contained := map[string]any{
		"resourceType": "Library",
		"url":          "http://example.org/Library/Demo",
		"name":         "Demo",
		"version":      "1.0.0",
		"content": []any{
			map[string]any{
				"contentType": "application/elm+json",
				"data":        base64.StdEncoding.EncodeToString([]byte(demoELMJSON())),
			},
		},
	}
	got, err := provider.EvaluateCQL(context.Background(), "Given", Request{
		Language:  sdc.CQLIdentifierLanguage,
		Patient:   adaPatient(t),
		Contained: []map[string]any{contained},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "Ada" {
		t.Fatalf("contained ELM: %#v", got)
	}
}

func libraryContentEnvelope(t *testing.T, content []map[string]any) *types.ResourceEnvelope {
	t.Helper()
	obj := map[string]any{
		"resourceType": "Library",
		"id":           "demo",
		"url":          "http://example.org/Library/Demo",
		"name":         "Demo",
		"version":      "1.0.0",
		"status":       "active",
		"content":      content,
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	env, err := types.NewJSONCodec().ParseJSON("Library", raw)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestExplicitCQLWinsOverEarlierELM(t *testing.T) {
	cqlSrc := "library Demo version '1.0.0'\ncontext Patient\ndefine \"X\": false\n"
	env := libraryContentEnvelope(t, []map[string]any{
		{"contentType": "application/elm+json", "data": base64.StdEncoding.EncodeToString([]byte(demoELMJSON()))},
		{"contentType": "text/cql", "data": base64.StdEncoding.EncodeToString([]byte(cqlSrc))},
	})
	src, _, _, _, err := EnvelopeLibrary(env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, `define "X": false`) {
		t.Fatalf("expected explicit CQL, got %s", src)
	}
	eng := testEngine(t)
	lib, err := eng.CompileLibrary(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "X", EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("explicit CQL should win: %#v", got)
	}
}

func TestRecoveredInvalidCQLFallsBackToELM(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"library": map[string]any{
			"identifier": map[string]any{"id": "Demo", "version": "1.0.0"},
			"cql":        "library Broken version '1.0.0'\ncontext Patient\ndefine \"X\": this is not valid cql\n",
			"statements": map[string]any{
				"def": []any{
					map[string]any{
						"name":    "X",
						"context": "Patient",
						"expression": map[string]any{
							"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Boolean", "value": "true",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	env := elmLibraryEnvelope(t, string(payload))
	eng := testEngine(t)
	lib, err := eng.CompileLibrary(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "X", EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("ELM fallback: %#v", got)
	}
}

func TestAnnotationRecoveryIgnoresStatementLocators(t *testing.T) {
	payload := `{
		"library": {
			"identifier": {"id": "Ann", "version": "1.0.0"},
			"annotation": [{
				"type": "Annotation",
				"s": {
					"s": [
						{"value": ["library Ann version '1.0.0'\n"]},
						{"value": ["context Patient\n"]},
						{"value": ["define \"X\": true\n"]}
					]
				}
			}],
			"statements": {
				"def": [{
					"name": "X",
					"context": "Patient",
					"annotation": [{
						"type": "Annotation",
						"s": {"s": [{"value": ["define \"Scrambled\": garbage\n"]}]}
					}],
					"expression": {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Boolean", "value": "true"}
				}]
			}
		}
	}`
	env := elmLibraryEnvelope(t, payload)
	src, _, _, _, err := EnvelopeLibrary(env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(src, "Scrambled") {
		t.Fatalf("statement annotations leaked into recovered CQL: %s", src)
	}
	if !strings.Contains(src, `define "X": true`) {
		t.Fatalf("library annotation source: %s", src)
	}
}

func TestCQLRecoveryIgnoresStatementLibraryStrings(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"library": map[string]any{
			"identifier": map[string]any{"id": "Ann", "version": "1.0.0"},
			"cql":        "library Ann version '1.0.0'\ncontext Patient\ndefine \"X\": true\n",
			"statements": map[string]any{
				"def": []any{
					map[string]any{
						"name": "X",
						"cql":  "library Scrambled version '1.0.0'\ncontext Patient\ndefine \"Scrambled\": false\ndefine \"More\": 1\n",
						"expression": map[string]any{
							"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Boolean", "value": "true",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	env := elmLibraryEnvelope(t, string(payload))
	src, _, _, _, err := EnvelopeLibrary(env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(src, "Scrambled") {
		t.Fatalf("statement library string leaked into recovery: %s", src)
	}
	if !strings.Contains(src, `define "X": true`) {
		t.Fatalf("library-level cql: %s", src)
	}
}

func TestInvalidIntervalBoundFails(t *testing.T) {
	_, err := parseELMExpr(map[string]any{
		"type": "Interval",
		"low":  "broken",
		"high": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "10"},
	})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected invalid low bound error, got %v", err)
	}
	n, err := parseELMExpr(map[string]any{
		"type": "Interval",
		"high": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	iv, ok := n.(*intervalNode)
	if !ok || iv.low != nil || iv.high == nil {
		t.Fatalf("missing low should be nil: %#v", n)
	}
}

func TestInvalidIfElseFails(t *testing.T) {
	_, err := parseELMExpr(map[string]any{
		"type":      "If",
		"condition": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Boolean", "value": "true"},
		"then":      map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"},
		"else":      "broken",
	})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected invalid else error, got %v", err)
	}
}

func TestInvalidCaseElseFails(t *testing.T) {
	_, err := parseELMExpr(map[string]any{
		"type": "Case",
		"caseItem": []any{
			map[string]any{
				"when": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Boolean", "value": "true"},
				"then": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"},
			},
		},
		"else": "broken",
	})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected invalid case else error, got %v", err)
	}
	n, err := parseELMExpr(map[string]any{
		"type": "Case",
		"caseItem": []any{
			map[string]any{
				"when": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Boolean", "value": "true"},
				"then": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := n.(*caseNode)
	if !ok || c.elseN != nil {
		t.Fatalf("missing case else should be nil: %#v", n)
	}
}

func TestCalculateAgeUsesOperandUnlessPatientBirthDate(t *testing.T) {
	src := `{
		"library": {
			"identifier": {"id": "AgeLib", "version": "1.0.0"},
			"statements": {"def": [
				{
					"name": "FromBirth",
					"context": "Patient",
					"expression": {
						"type": "CalculateAge",
						"precision": "Year",
						"operand": {"type": "Property", "path": "birthDate", "source": {"type": "ExpressionRef", "name": "Patient"}}
					}
				},
				{
					"name": "FromDate",
					"context": "Patient",
					"expression": {
						"type": "CalculateAge",
						"precision": "Year",
						"operand": {"type": "Date", "year": 2010, "month": 1, "day": 1}
					}
				}
			]}
		}
	}`
	eng := testEngine(t)
	lib, err := eng.ParseELM([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	env := EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}}
	got, err := eng.EvalDefine(context.Background(), lib, "FromBirth", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(26) {
		t.Fatalf("Patient birthDate age: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "FromDate", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(16) {
		t.Fatalf("CalculateAge of 2010-01-01: %#v", got)
	}
}

func TestCalculateAgeBirthDateNonYearPrecision(t *testing.T) {
	src := `{
		"library": {
			"identifier": {"id": "AgeLib", "version": "1.0.0"},
			"statements": {"def": [{
				"name": "Months",
				"context": "Patient",
				"expression": {
					"type": "CalculateAge",
					"precision": "Month",
					"operand": {"type": "Property", "path": "birthDate", "source": {"type": "ExpressionRef", "name": "Patient"}}
				}
			}]}
		}
	}`
	eng := testEngine(t)
	lib, err := eng.ParseELM([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "Months", EvalContext{Patient: adaPatient(t), Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(320) {
		t.Fatalf("CalculateAge in months of birthDate: %#v", got)
	}
}

func TestELMLibraryContextPrefersDeclaredThenPatient(t *testing.T) {
	declared := `{
		"library": {
			"identifier": {"id": "Ctx", "version": "1.0.0"},
			"contexts": {"def": [{"name": "Patient"}]},
			"statements": {"def": [
				{"name": "X", "context": "Patient", "expression": {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Boolean", "value": "true"}},
				{"name": "Y", "context": "Unfiltered", "expression": {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"}}
			]}
		}
	}`
	lib, err := parseELMLibrary([]byte(declared))
	if err != nil {
		t.Fatal(err)
	}
	if lib.Context != "Patient" {
		t.Fatalf("contexts.def should win, got %q", lib.Context)
	}
	mixed := `{
		"library": {
			"identifier": {"id": "Ctx", "version": "1.0.0"},
			"statements": {"def": [
				{"name": "X", "context": "Patient", "expression": {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Boolean", "value": "true"}},
				{"name": "Y", "context": "Unfiltered", "expression": {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"}}
			]}
		}
	}`
	lib, err = parseELMLibrary([]byte(mixed))
	if err != nil {
		t.Fatal(err)
	}
	if lib.Context != "Patient" {
		t.Fatalf("any Patient statement should win, got %q", lib.Context)
	}
	unfiltered := `{
		"library": {
			"identifier": {"id": "Ctx", "version": "1.0.0"},
			"statements": {"def": [
				{"name": "Y", "context": "Unfiltered", "expression": {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"}}
			]}
		}
	}`
	lib, err = parseELMLibrary([]byte(unfiltered))
	if err != nil {
		t.Fatal(err)
	}
	if lib.Context != "Unfiltered" {
		t.Fatalf("unfiltered-only: got %q", lib.Context)
	}
	mixedDecl := `{
		"library": {
			"identifier": {"id": "Ctx", "version": "1.0.0"},
			"contexts": {"def": [{"name": "Patient"}, {"name": "Unfiltered"}]},
			"statements": {"def": [
				{"name": "Y", "context": "Unfiltered", "expression": {"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"}}
			]}
		}
	}`
	lib, err = parseELMLibrary([]byte(mixedDecl))
	if err != nil {
		t.Fatal(err)
	}
	if lib.Context != "Patient" {
		t.Fatalf("declared Patient should win over later Unfiltered, got %q", lib.Context)
	}
}

func TestELMRetrieveCodesList(t *testing.T) {
	n, err := parseELMExpr(map[string]any{
		"type":     "Retrieve",
		"dataType": "{http://hl7.org/fhir}Observation",
		"codes": map[string]any{
			"type": "List",
			"element": []any{
				map[string]any{"type": "CodeRef", "name": "HeartRate"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := n.(*retrieveNode)
	if !ok {
		t.Fatalf("retrieve: %#v", n)
	}
	if r.terminology != "HeartRate" || r.comparator != "=" {
		t.Fatalf("list codes: %+v", r)
	}
}

func TestELMRetrieveCodesFunctionRefAndLiteral(t *testing.T) {
	n, err := parseELMExpr(map[string]any{
		"type":     "Retrieve",
		"dataType": "{http://hl7.org/fhir}Observation",
		"codes": map[string]any{
			"type": "ToList",
			"operand": map[string]any{
				"type":        "FunctionRef",
				"name":        "ToConcept",
				"libraryName": "FHIRHelpers",
				"operand": []any{
					map[string]any{"type": "CodeRef", "name": "HeartRate"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := n.(*retrieveNode)
	if !ok {
		t.Fatalf("retrieve: %#v", n)
	}
	if r.terminology != "HeartRate" || r.comparator != "=" {
		t.Fatalf("FunctionRef ToConcept codes: %+v", r)
	}
	n, err = parseELMExpr(map[string]any{
		"type":     "Retrieve",
		"dataType": "{http://hl7.org/fhir}Observation",
		"codes": map[string]any{
			"type":   "Code",
			"code":   "8867-4",
			"system": "http://loinc.org",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, ok = n.(*retrieveNode)
	if !ok {
		t.Fatalf("retrieve: %#v", n)
	}
	if r.terminology != "http://loinc.org|8867-4" || r.comparator != "=" {
		t.Fatalf("Code literal: %+v", r)
	}
}

func TestELMSameAsUsesPrecision(t *testing.T) {
	got := evalELMExpr(t, map[string]any{
		"type":      "SameAs",
		"precision": "Year",
		"operand": []any{
			map[string]any{"type": "Date", "year": 2020, "month": 1, "day": 1},
			map[string]any{"type": "Date", "year": 2020, "month": 6, "day": 15},
		},
	})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("same year as: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type": "SameAs",
		"operand": []any{
			map[string]any{"type": "Date", "year": 2020, "month": 1, "day": 1},
			map[string]any{"type": "Date", "year": 2020, "month": 6, "day": 15},
		},
	})
	if len(got) != 1 || got[0] != false {
		t.Fatalf("same as full date: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":      "SameOrBefore",
		"precision": "Year",
		"operand": []any{
			map[string]any{"type": "Date", "year": 2019, "month": 12, "day": 31},
			map[string]any{"type": "Date", "year": 2020, "month": 1, "day": 1},
		},
	})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("same year or before: %#v", got)
	}
}

func TestELMQueryReturnDistinctDefaultsTrue(t *testing.T) {
	src := map[string]any{
		"type": "Query",
		"source": []any{
			map[string]any{
				"alias": "X",
				"expression": map[string]any{
					"type": "List",
					"element": []any{
						map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"},
						map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"},
						map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "2"},
					},
				},
			},
		},
		"return": map[string]any{
			"expression": map[string]any{"type": "AliasRef", "name": "X"},
		},
	}
	n, err := parseELMExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	q, ok := n.(*queryNode)
	if !ok || !q.distinct {
		t.Fatalf("missing distinct should default true: %#v", n)
	}
	got := evalELMExpr(t, src)
	if len(got) != 2 {
		t.Fatalf("distinct return: %#v", got)
	}
	src["return"] = map[string]any{
		"distinct":   false,
		"expression": map[string]any{"type": "AliasRef", "name": "X"},
	}
	got = evalELMExpr(t, src)
	if len(got) != 3 {
		t.Fatalf("return all: %#v", got)
	}
}

func TestELMFilterForEachBindScope(t *testing.T) {
	list := map[string]any{
		"type": "List",
		"element": []any{
			map[string]any{
				"type": "Tuple",
				"element": []any{
					map[string]any{"name": "status", "value": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}String", "value": "final"}},
					map[string]any{"name": "id", "value": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}String", "value": "a"}},
				},
			},
			map[string]any{
				"type": "Tuple",
				"element": []any{
					map[string]any{"name": "status", "value": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}String", "value": "cancelled"}},
					map[string]any{"name": "id", "value": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}String", "value": "b"}},
				},
			},
		},
	}
	got := evalELMExpr(t, map[string]any{
		"type":   "Filter",
		"source": list,
		"scope":  "X",
		"condition": map[string]any{
			"type": "Equal",
			"operand": []any{
				map[string]any{"type": "Property", "path": "status", "scope": "X"},
				map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}String", "value": "final"},
			},
		},
	})
	if len(got) != 1 {
		t.Fatalf("filter: %#v", got)
	}
	obj, ok := got[0].(map[string]any)
	if !ok || obj["id"] != "a" {
		t.Fatalf("filter row: %#v", got[0])
	}
	got = evalELMExpr(t, map[string]any{
		"type":   "ForEach",
		"source": list,
		"scope":  "X",
		"element": map[string]any{
			"type": "Property", "path": "id", "scope": "X",
		},
	})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("forEach: %#v", got)
	}
}

func TestELMOverlapsBeforeAfter(t *testing.T) {
	intLit := func(v string) map[string]any {
		return map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": v}
	}
	interval := func(low, high string) map[string]any {
		return map[string]any{"type": "Interval", "low": intLit(low), "high": intLit(high)}
	}
	got := evalELMExpr(t, map[string]any{"type": "OverlapsBefore", "operand": []any{interval("1", "5"), interval("3", "8")}})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("overlaps before: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "OverlapsBefore", "operand": []any{interval("3", "8"), interval("1", "5")}})
	if len(got) != 1 || got[0] != false {
		t.Fatalf("overlaps before reversed: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "OverlapsAfter", "operand": []any{interval("3", "8"), interval("1", "5")}})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("overlaps after: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Overlaps", "operand": []any{interval("1", "5"), interval("3", "8")}})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("overlaps: %#v", got)
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Interval[1, 5] overlaps before Interval[3, 8]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("CQL overlaps before: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Interval[3, 8] overlaps after Interval[1, 5]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("CQL overlaps after: %#v", got)
	}
}

func evalELMExpr(t *testing.T, obj map[string]any) []any {
	t.Helper()
	n, err := parseELMExpr(obj)
	if err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t)
	got, err := eng.evalNode(context.Background(), n, EvalContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestELMMeetsBeforeAfter(t *testing.T) {
	intLit := func(v string) map[string]any {
		return map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": v}
	}
	interval := func(low, high string) map[string]any {
		return map[string]any{"type": "Interval", "low": intLit(low), "high": intLit(high)}
	}
	got := evalELMExpr(t, map[string]any{"type": "MeetsBefore", "operand": []any{interval("1", "5"), interval("5", "10")}})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("meets before: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "MeetsAfter", "operand": []any{interval("5", "10"), interval("1", "5")}})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("meets after: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "MeetsBefore", "operand": []any{interval("5", "10"), interval("1", "5")}})
	if len(got) != 1 || got[0] != false {
		t.Fatalf("meets before reversed: %#v", got)
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Interval[1, 5] meets before Interval[5, 10]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("CQL meets before: %#v", got)
	}
}

func TestELMBeforeYearOf(t *testing.T) {
	date := func(y, m, d int) map[string]any {
		return map[string]any{"type": "Date", "year": y, "month": m, "day": d}
	}
	got := evalELMExpr(t, map[string]any{
		"type": "Before", "precision": "Year",
		"operand": []any{date(2019, 12, 31), date(2020, 6, 1)},
	})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("before year of: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type": "Before", "precision": "Year",
		"operand": []any{date(2020, 1, 1), date(2020, 12, 31)},
	})
	if len(got) != 1 || got[0] != false {
		t.Fatalf("same year is not before year of: %#v", got)
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "@2019-12-31 before year of @2020-06-01", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("CQL before year of: %#v", got)
	}
}

func TestELMSortByDirection(t *testing.T) {
	got := evalELMExpr(t, map[string]any{
		"type": "Query",
		"source": []any{map[string]any{
			"alias": "X",
			"expression": map[string]any{
				"type": "List",
				"element": []any{
					map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "2"},
					map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "3"},
					map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"},
				},
			},
		}},
		"sort": map[string]any{
			"by": []any{map[string]any{"type": "ByDirection", "direction": "desc"}},
		},
	})
	if len(got) != 3 || got[0] != int64(3) || got[1] != int64(2) || got[2] != int64(1) {
		t.Fatalf("ByDirection desc: %#v", got)
	}
}

func TestELMIndexerSourceIndex(t *testing.T) {
	got := evalELMExpr(t, map[string]any{
		"type": "Indexer",
		"source": map[string]any{
			"type": "List",
			"element": []any{
				map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}String", "value": "a"},
				map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}String", "value": "b"},
			},
		},
		"index": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": "1"},
	})
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("named Indexer: %#v", got)
	}
}

func TestELMRetrieveDateWindow(t *testing.T) {
	in, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation", "id": "in", "status": "final",
		"code": {"text": "HR"}, "effectiveDateTime": "2020-06-01", "subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation", "id": "out", "status": "final",
		"code": {"text": "HR"}, "effectiveDateTime": "2018-01-01", "subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	n, err := parseELMExpr(map[string]any{
		"type":         "Retrieve",
		"dataType":     "{http://hl7.org/fhir}Observation",
		"dateProperty": "effective",
		"dateLow":      map[string]any{"type": "Date", "year": 2020, "month": 1, "day": 1},
		"dateHigh":     map[string]any{"type": "Date", "year": 2020, "month": 12, "day": 31},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := n.(*retrieveNode)
	if !ok || r.datePath != "effective" || r.dateLow == nil || r.dateHigh == nil {
		t.Fatalf("retrieve date fields: %#v", n)
	}
	eng, err := NewEngine(Config{Retriever: StaticRetriever{in, out}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.evalNode(context.Background(), n, EvalContext{Patient: adaPatient(t), Retriever: StaticRetriever{in, out}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("date window: %#v", got)
	}
	obj, _ := asObject(got[0])
	if obj["id"] != "in" {
		t.Fatalf("date window kept wrong resource: %#v", got[0])
	}
}

func TestELMRetrievePluralDateBoundsMatchNone(t *testing.T) {
	in, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation", "id": "in", "status": "final",
		"code": {"text": "HR"}, "effectiveDateTime": "2020-06-01", "subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	n, err := parseELMExpr(map[string]any{
		"type":         "Retrieve",
		"dataType":     "{http://hl7.org/fhir}Observation",
		"dateProperty": "effective",
		"dateLow": map[string]any{
			"type": "List",
			"element": []any{
				map[string]any{"type": "Date", "year": 2020, "month": 1, "day": 1},
				map[string]any{"type": "Date", "year": 2019, "month": 1, "day": 1},
			},
		},
		"dateHigh": map[string]any{"type": "Date", "year": 2020, "month": 12, "day": 31},
	})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{Retriever: StaticRetriever{in}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.evalNode(context.Background(), n, EvalContext{Patient: adaPatient(t), Retriever: StaticRetriever{in}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("plural dateLow must match none: %#v", got)
	}
}

func TestELMToListNullAndUnknownType(t *testing.T) {
	n, err := parseELMExpr(map[string]any{"type": "ToList", "operand": map[string]any{"type": "Null"}})
	if err != nil {
		t.Fatal(err)
	}
	u, ok := n.(*unaryNode)
	if !ok || u.op != "tolist" {
		t.Fatalf("ToList should wrap, got %#v", n)
	}
	got := evalELMExpr(t, map[string]any{"type": "ToList", "operand": map[string]any{"type": "Null"}})
	if len(got) != 1 {
		t.Fatalf("ToList(null) should be singleton empty list, got %#v", got)
	}
	list, ok := got[0].([]any)
	if !ok || len(list) != 0 {
		t.Fatalf("ToList(null) should be empty list, got %#v", got)
	}
	_, err = parseELMExpr(map[string]any{"type": "NotARealOperator", "operand": map[string]any{"type": "Null"}})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unknown ELM type should fail at compile, got %v", err)
	}
}

func TestParseELMRespectsMaxExpressionLen(t *testing.T) {
	eng, err := NewEngine(Config{MaxExpressionLen: 8})
	if err != nil {
		t.Fatal(err)
	}
	_, err = eng.ParseELM([]byte(`{"library":{"identifier":{"id":"X"}}}`))
	if err == nil || !strings.Contains(err.Error(), "maximum length") {
		t.Fatalf("ParseELM should enforce maxExpressionLen, got %v", err)
	}
}

func elmIntLit(v string) map[string]any {
	return map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Integer", "value": v}
}

func elmBoolLit(v string) map[string]any {
	return map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Boolean", "value": v}
}

func TestELMToTimeAndCoalesce(t *testing.T) {
	got := evalELMExpr(t, map[string]any{
		"type": "ToTime",
		"operand": map[string]any{
			"type": "DateTime", "year": 2021, "month": 6, "day": 15,
			"hour": 14, "minute": 30, "second": 45, "millisecond": 0,
		},
	})
	if len(got) != 1 {
		t.Fatalf("ToTime: %#v", got)
	}
	tm, ok := asTime(got[0])
	if !ok || tm.Year() != 2026 || tm.Month() != time.September || tm.Day() != 21 ||
		tm.Hour() != 14 || tm.Minute() != 30 || tm.Second() != 45 {
		t.Fatalf("ToTime should overlay clock date, got %v", tm)
	}
	got = evalELMExpr(t, map[string]any{
		"type":    "ToTime",
		"operand": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}String", "value": "@T08:15:00"},
	})
	if len(got) != 1 {
		t.Fatalf("ToTime string: %#v", got)
	}
	tm, ok = asTime(got[0])
	if !ok || tm.Hour() != 8 || tm.Minute() != 15 {
		t.Fatalf("ToTime string: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type": "Coalesce",
		"operand": []any{
			map[string]any{"type": "Null"},
			elmIntLit("7"),
			elmIntLit("9"),
		},
	})
	if len(got) != 1 || got[0] != int64(7) {
		t.Fatalf("Coalesce: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Coalesce", "operand": []any{map[string]any{"type": "Null"}}})
	if got != nil && len(got) != 0 {
		t.Fatalf("Coalesce all null: %#v", got)
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Coalesce(null, 4, 5)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(4) {
		t.Fatalf("CQL Coalesce: %#v", got)
	}
}

func TestELMDateTimeTimezoneOffset(t *testing.T) {
	n, err := parseELMExpr(map[string]any{
		"type": "DateTime", "year": 2021, "month": 1, "day": 1,
		"hour": 8, "minute": 0, "second": 0, "millisecond": 0,
		"timezoneOffset": -5,
	})
	if err != nil {
		t.Fatal(err)
	}
	call, ok := n.(*callNode)
	if !ok || len(call.args) != 8 {
		t.Fatalf("DateTime should pass timezoneOffset as 8th arg, got %#v", n)
	}
	got := evalELMExpr(t, map[string]any{
		"type": "DateTime", "year": 2021, "month": 1, "day": 1,
		"hour": 8, "minute": 0, "second": 0, "millisecond": 0,
		"timezoneOffset": -5,
	})
	if len(got) != 1 {
		t.Fatalf("DateTime offset: %#v", got)
	}
	tm, ok := asTime(got[0])
	if !ok {
		t.Fatalf("DateTime offset not a time: %#v", got[0])
	}
	_, off := tm.Zone()
	if off != -5*3600 || tm.Hour() != 8 {
		t.Fatalf("timezoneOffset ignored: %v offset=%d", tm, off)
	}
	got = evalELMExpr(t, map[string]any{
		"type": "DateTime", "year": 2021, "month": 1, "day": 1,
		"hour": 8, "minute": 0, "second": 0, "millisecond": 0,
		"timezoneOffset": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Decimal", "value": "-5"},
	})
	tm, ok = asTime(got[0])
	if !ok {
		t.Fatalf("DateTime offset expression: %#v", got)
	}
	_, off = tm.Zone()
	if off != -5*3600 {
		t.Fatalf("timezoneOffset expression ignored: %v offset=%d", tm, off)
	}
}

func TestELMRetrieveDateClosedBounds(t *testing.T) {
	onBound, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation", "id": "on", "status": "final",
		"code": {"text": "HR"}, "effectiveDateTime": "2020-01-01", "subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	inside, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation", "id": "in", "status": "final",
		"code": {"text": "HR"}, "effectiveDateTime": "2020-06-01", "subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	n, err := parseELMExpr(map[string]any{
		"type":           "Retrieve",
		"dataType":       "{http://hl7.org/fhir}Observation",
		"dateProperty":   "effective",
		"dateLow":        map[string]any{"type": "Date", "year": 2020, "month": 1, "day": 1},
		"dateHigh":       map[string]any{"type": "Date", "year": 2020, "month": 12, "day": 31},
		"dateLowClosed":  false,
		"dateHighClosed": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := n.(*retrieveNode)
	if !ok || r.dateLowClosed || !r.dateHighClosed {
		t.Fatalf("retrieve date closed flags: %#v", n)
	}
	eng, err := NewEngine(Config{Retriever: StaticRetriever{onBound, inside}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.evalNode(context.Background(), n, EvalContext{Patient: adaPatient(t), Retriever: StaticRetriever{onBound, inside}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("open low bound should drop the low instant: %#v", got)
	}
	obj, _ := asObject(got[0])
	if obj["id"] != "in" {
		t.Fatalf("open low bound kept wrong resource: %#v", got[0])
	}

	n, err = parseELMExpr(map[string]any{
		"type":                     "Retrieve",
		"dataType":                 "{http://hl7.org/fhir}Observation",
		"dateProperty":             "effective",
		"dateLow":                  map[string]any{"type": "Date", "year": 2020, "month": 1, "day": 1},
		"dateHigh":                 map[string]any{"type": "Date", "year": 2020, "month": 12, "day": 31},
		"dateLowClosedExpression":  elmBoolLit("false"),
		"dateHighClosedExpression": elmBoolLit("true"),
	})
	if err != nil {
		t.Fatal(err)
	}
	r, ok = n.(*retrieveNode)
	if !ok || r.dateLowClosedExpr == nil || r.dateHighClosedExpr == nil {
		t.Fatalf("retrieve date closed expressions: %#v", n)
	}
	got, err = eng.evalNode(context.Background(), n, EvalContext{Patient: adaPatient(t), Retriever: StaticRetriever{onBound, inside}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("open lowClosedExpression should drop the low instant: %#v", got)
	}
	obj, _ = asObject(got[0])
	if obj["id"] != "in" {
		t.Fatalf("lowClosedExpression kept wrong resource: %#v", got[0])
	}
}

func TestELMPatientRetrieveAppliesDateWindow(t *testing.T) {
	n, err := parseELMExpr(map[string]any{
		"type":         "Retrieve",
		"dataType":     "{http://hl7.org/fhir}Patient",
		"dateProperty": "birthDate",
		"dateLow":      map[string]any{"type": "Date", "year": 1990, "month": 1, "day": 1},
		"dateHigh":     map[string]any{"type": "Date", "year": 1999, "month": 12, "day": 31},
	})
	if err != nil {
		t.Fatal(err)
	}
	eng := testEngine(t)
	got, err := eng.evalNode(context.Background(), n, EvalContext{Patient: adaPatient(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Patient retrieve date window should filter the context Patient, got %#v", got)
	}
	n, err = parseELMExpr(map[string]any{
		"type":         "Retrieve",
		"dataType":     "{http://hl7.org/fhir}Patient",
		"dateProperty": "birthDate",
		"dateLow":      map[string]any{"type": "Date", "year": 2000, "month": 1, "day": 1},
		"dateHigh":     map[string]any{"type": "Date", "year": 2000, "month": 12, "day": 31},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = eng.evalNode(context.Background(), n, EvalContext{Patient: adaPatient(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Patient retrieve in window: %#v", got)
	}
}

func TestELMIntervalClosedExpression(t *testing.T) {
	n, err := parseELMExpr(map[string]any{
		"type":                 "Interval",
		"low":                  elmIntLit("1"),
		"high":                 elmIntLit("5"),
		"lowClosedExpression":  elmBoolLit("false"),
		"highClosedExpression": elmBoolLit("true"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ivn, ok := n.(*intervalNode)
	if !ok || ivn.lowClosedExpr == nil || ivn.highClosedExpr == nil {
		t.Fatalf("Interval closed expressions: %#v", n)
	}
	got := evalELMExpr(t, map[string]any{
		"type": "Contains",
		"operand": []any{
			map[string]any{
				"type":                 "Interval",
				"low":                  elmIntLit("1"),
				"high":                 elmIntLit("5"),
				"lowClosedExpression":  elmBoolLit("false"),
				"highClosedExpression": elmBoolLit("true"),
			},
			elmIntLit("1"),
		},
	})
	if len(got) != 1 || got[0] != false {
		t.Fatalf("open lowClosedExpression should exclude 1: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type": "Contains",
		"operand": []any{
			map[string]any{
				"type":                 "Interval",
				"low":                  elmIntLit("1"),
				"high":                 elmIntLit("5"),
				"lowClosedExpression":  elmBoolLit("false"),
				"highClosedExpression": elmBoolLit("true"),
			},
			elmIntLit("2"),
		},
	})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("open lowClosedExpression should include 2: %#v", got)
	}
}
