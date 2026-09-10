package conceptmap

import (
	"context"
	"strings"
	"testing"
)

func TestTranslatorMapsSourceCoding(t *testing.T) {
	m := Map{
		URL: "http://example.org/maps/gender",
		Group: []Group{{
			Source: "http://example.org/source",
			Target: "http://example.org/target",
			Element: []Element{{
				Code: "M",
				Target: []Target{{
					Code:        "male",
					Display:     "Male",
					Equivalence: "equivalent",
				}},
			}},
		}},
	}
	translator := Translator{Resolver: StaticResolver{m.URL: m}}
	codings, err := translator.Translate(context.Background(), TranslateRequest{
		MapCanonical: m.URL,
		Source:       map[string]any{"system": "http://example.org/source", "code": "M"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(codings) != 1 || codings[0]["code"] != "male" {
		t.Fatalf("unexpected translation: %#v", codings)
	}
}

func TestTranslatorRejectsNoMap(t *testing.T) {
	m := Map{
		URL: "http://example.org/maps/gender",
		Group: []Group{{
			Element: []Element{{Code: "M", NoMap: true}},
		}},
	}
	translator := Translator{Resolver: StaticResolver{m.URL: m}}
	_, err := translator.Translate(context.Background(), TranslateRequest{
		MapCanonical: m.URL,
		Source:       map[string]any{"code": "M"},
	})
	if err == nil || !strings.Contains(err.Error(), "no-map") {
		t.Fatalf("expected no-map error, got %v", err)
	}
}

func TestTranslatorUsesUnmappedProvided(t *testing.T) {
	m := Map{
		URL: "http://example.org/maps/status",
		Group: []Group{{
			Source: "http://example.org/source",
			Target: "http://example.org/target",
			Unmapped: &Unmapped{
				Mode: "provided",
			},
		}},
	}
	translator := Translator{Resolver: StaticResolver{m.URL: m}}
	codings, err := translator.Translate(context.Background(), TranslateRequest{
		MapCanonical: m.URL,
		Source:       map[string]any{"system": "http://example.org/source", "code": "local", "display": "Local"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(codings) != 1 || codings[0]["code"] != "local" || codings[0]["system"] != "http://example.org/source" {
		t.Fatalf("unexpected provided translation: %#v", codings)
	}
}

func TestTranslatorUsesUnmappedUseSourceCode(t *testing.T) {
	m := Map{
		URL: "http://example.org/maps/status",
		Group: []Group{{
			Target: "http://example.org/target",
			Unmapped: &Unmapped{
				Mode: "use-source-code",
			},
		}},
	}
	translator := Translator{Resolver: StaticResolver{m.URL: m}}
	codings, err := translator.Translate(context.Background(), TranslateRequest{
		MapCanonical: m.URL,
		Source:       map[string]any{"code": "local"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(codings) != 1 || codings[0]["code"] != "local" || codings[0]["system"] != "http://example.org/target" {
		t.Fatalf("unexpected use-source-code translation: %#v", codings)
	}
}

func TestTranslatorRejectsUnmappedDisabled(t *testing.T) {
	m := Map{
		URL: "http://example.org/maps/status",
		Group: []Group{{
			Unmapped: &Unmapped{Mode: "disabled"},
		}},
	}
	translator := Translator{Resolver: StaticResolver{m.URL: m}}
	_, err := translator.Translate(context.Background(), TranslateRequest{
		MapCanonical: m.URL,
		Source:       map[string]any{"code": "missing"},
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled unmapped error, got %v", err)
	}
}

func TestTranslatorAppliesUnmappedOnlyOnceAcrossGroups(t *testing.T) {
	m := Map{
		URL: "http://example.org/maps/status",
		Group: []Group{
			{
				Target: "http://example.org/target-a",
				Unmapped: &Unmapped{Mode: "provided"},
			},
			{
				Target: "http://example.org/target-b",
				Unmapped: &Unmapped{Mode: "provided"},
			},
		},
	}
	translator := Translator{Resolver: StaticResolver{m.URL: m}}
	codings, err := translator.Translate(context.Background(), TranslateRequest{
		MapCanonical: m.URL,
		Source:       map[string]any{"code": "missing", "system": "http://example.org/source"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(codings) != 1 {
		t.Fatalf("expected one unmapped coding, got %#v", codings)
	}
}

func TestTranslatorUsesUnmappedFixed(t *testing.T) {
	m := Map{
		URL: "http://example.org/maps/status",
		Group: []Group{{
			Target: "http://example.org/target",
			Unmapped: &Unmapped{
				Mode:    "fixed",
				Code:    "unknown",
				Display: "Unknown",
			},
		}},
	}
	translator := Translator{Resolver: StaticResolver{m.URL: m}}
	codings, err := translator.Translate(context.Background(), TranslateRequest{
		MapCanonical: m.URL,
		Source:       map[string]any{"code": "missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(codings) != 1 || codings[0]["code"] != "unknown" {
		t.Fatalf("unexpected unmapped translation: %#v", codings)
	}
}
