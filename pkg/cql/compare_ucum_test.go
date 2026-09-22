package cql

import (
	"context"
	"testing"
)

type stubUCUM struct {
	factor float64
}

func (s stubUCUM) Canonicalize(unit string) (string, bool) { return unit, true }
func (s stubUCUM) Dimension(unit string) (string, bool) {
	if unit == "custom_u" {
		return "g", true
	}
	return defaultUCUM.Dimension(unit)
}
func (s stubUCUM) Convert(value float64, fromUnit, toUnit string) (float64, bool) {
	if fromUnit == "custom_u" && toUnit == "g" {
		return value * s.factor, true
	}
	return defaultUCUM.Convert(value, fromUnit, toUnit)
}

func TestQuantityRatioTermVolumeUCUM(t *testing.T) {
	conv := DefaultUCUMConverter()
	v, ok := quantityRatioTerm(Quantity{Value: 2, Unit: "mL"}, Quantity{Value: 2000, Unit: "uL"}, conv)
	if !ok {
		t.Fatal("quantityRatioTerm mL*uL")
	}
	if v < 3.99 || v > 4.01 {
		t.Fatalf("expected ~4 mL^2 value term, got %v", v)
	}
}

func TestEngineCompareUsesConfigUCUM(t *testing.T) {
	eng, err := NewEngine(Config{UCUM: stubUCUM{factor: 2}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.Eval(context.Background(), "1 'custom_u' ~ 2000 'mg'", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("expected custom_u equivalence via engine UCUM: %#v", got)
	}
}
