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
