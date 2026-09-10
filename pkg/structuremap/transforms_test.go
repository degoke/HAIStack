package structuremap

import (
	"context"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/conceptmap"
)

func TestIdTransformCreatesIdentifier(t *testing.T) {
	value, err := Engine{}.applyTransform(context.Background(), "id", []Parameter{
		{ValueString: "http://example.org/mrn"},
		{ValueString: "123"},
		{ValueString: "MR"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	identifier, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected Identifier map, got %T", value)
	}
	if identifier["system"] != "http://example.org/mrn" || identifier["value"] != "123" {
		t.Fatalf("unexpected identifier: %#v", identifier)
	}
}

func TestQtyTransformParsesText(t *testing.T) {
	value, err := Engine{}.applyTransform(context.Background(), "qty", []Parameter{{ValueString: ">= 10 kg"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	quantity, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected Quantity map, got %T", value)
	}
	if quantity["comparator"] != ">=" || quantity["value"] != 10.0 || quantity["unit"] != "kg" {
		t.Fatalf("unexpected quantity: %#v", quantity)
	}
}

func TestCpTransformInfersEmailSystem(t *testing.T) {
	value, err := Engine{}.applyTransform(context.Background(), "cp", []Parameter{{ValueString: "ada@example.org"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	contact, ok := value.(map[string]any)
	if !ok || contact["system"] != "email" {
		t.Fatalf("unexpected contact point: %#v", value)
	}
}

func TestTruncateTransform(t *testing.T) {
	value, err := Engine{}.applyTransform(context.Background(), "truncate", []Parameter{
		{ValueString: "abcdef"},
		{ValueString: "3"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value != "abc" {
		t.Fatalf("unexpected truncate result: %v", value)
	}
}

func TestPointerTransform(t *testing.T) {
	value, err := Engine{}.applyTransform(context.Background(), "pointer", nil, nil, map[string]any{
		"resourceType": "Patient",
		"id":           "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if value != "Patient/abc" {
		t.Fatalf("unexpected pointer: %v", value)
	}
}

func TestTranslateTransformUsesConceptMap(t *testing.T) {
	m := conceptmap.Map{
		URL: "http://example.org/maps/gender",
		Group: []conceptmap.Group{{
			Source: "http://example.org/source",
			Target: "http://example.org/target",
			Element: []conceptmap.Element{{
				Code: "F",
				Target: []conceptmap.Target{{
					Code:        "female",
					Equivalence: "equivalent",
				}},
			}},
		}},
	}
	engine := Engine{
		Translator: conceptmap.Translator{Resolver: conceptmap.StaticResolver{m.URL: m}},
	}
	value, err := engine.applyTransform(context.Background(), "translate", []Parameter{
		{ValueString: m.URL},
	}, nil, map[string]any{"system": "http://example.org/source", "code": "F"})
	if err != nil {
		t.Fatal(err)
	}
	coding, ok := value.(map[string]any)
	if !ok || coding["code"] != "female" {
		t.Fatalf("unexpected translate result: %#v", value)
	}
}

func TestDateOpAddDays(t *testing.T) {
	value, err := Engine{}.applyTransform(context.Background(), "dateOp", []Parameter{
		{ValueString: "2020-01-01"},
		{ValueString: "add"},
		{ValueString: "P2D"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if value != "2020-01-03" {
		t.Fatalf("unexpected dateOp result: %v", value)
	}
}

func TestEscapeTransformJsonToPlain(t *testing.T) {
	value, err := Engine{}.applyTransform(context.Background(), "escape", []Parameter{
		{ValueString: "line\\nbreak"},
		{ValueString: "json"},
		{ValueString: "plain"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(value.(string), "\n") {
		t.Fatalf("expected unescaped newline, got %v", value)
	}
}
