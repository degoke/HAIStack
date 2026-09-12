package parquetfhir

import (
	"math/big"
	"testing"
)

func TestCanonicalizeQuantityCelsiusToKelvin(t *testing.T) {
	canonical, err := canonicalizeQuantity(map[string]any{
		"value":  36.5,
		"unit":   "C",
		"system": "http://unitsofmeasure.org",
		"code":   "Cel",
	})
	if err != nil {
		t.Fatalf("canonicalizeQuantity: %v", err)
	}
	if canonical == nil {
		t.Fatal("expected canonical quantity")
	}
	if got := canonical["value"]; got != "309.65" {
		t.Fatalf("value=%v, want 309.65", got)
	}
	if got := canonical["code"]; got != "K" {
		t.Fatalf("code=%v, want K", got)
	}
	if got := canonical["unit"]; got != "Kelvin" {
		t.Fatalf("unit=%v, want Kelvin", got)
	}
	if _, ok := canonical[quantityValueNumericField()].([]byte); !ok {
		t.Fatalf("expected %s bytes", quantityValueNumericField())
	}
}

func TestCanonicalizeQuantityFahrenheitToKelvin(t *testing.T) {
	canonical, err := canonicalizeQuantity(map[string]any{
		"value":  98.6,
		"unit":   "F",
		"system": "http://unitsofmeasure.org",
	})
	if err != nil {
		t.Fatalf("canonicalizeQuantity: %v", err)
	}
	if canonical == nil {
		t.Fatal("expected canonical quantity")
	}
	want := new(big.Rat)
	want.SetString("310.15")
	got, ok := new(big.Rat).SetString(canonical["value"].(string))
	if !ok || got.Cmp(want) != 0 {
		t.Fatalf("value=%v, want 310.15", canonical["value"])
	}
}

func TestCanonicalizeQuantityKelvinPassthrough(t *testing.T) {
	canonical, err := canonicalizeQuantity(map[string]any{
		"value":  300,
		"unit":   "K",
		"system": "http://unitsofmeasure.org",
		"code":   "K",
	})
	if err != nil {
		t.Fatalf("canonicalizeQuantity: %v", err)
	}
	if canonical["value"] != "300" {
		t.Fatalf("value=%v, want 300", canonical["value"])
	}
}

func TestNormalizeUCUMCodeFromUnit(t *testing.T) {
	if got := normalizeUCUMCode("", "C"); got != "Cel" {
		t.Fatalf("normalizeUCUMCode(C)=%q, want Cel", got)
	}
	if got := normalizeUCUMCode("", "°F"); got != "[degF]" {
		t.Fatalf("normalizeUCUMCode(°F)=%q, want [degF]", got)
	}
}
