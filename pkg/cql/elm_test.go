package cql

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
