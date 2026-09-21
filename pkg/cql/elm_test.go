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
