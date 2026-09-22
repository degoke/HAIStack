package cql

import (
	"context"
	"math"
	"testing"
)

func TestUCUMConverterFixtureUnits(t *testing.T) {
	conv := DefaultUCUMConverter()
	cases := []struct {
		value float64
		from  string
		to    string
		want  float64
	}{
		{1, "[ft_i]", "m", 0.3048},
		{5, "[in_i]", "cm", 12.7},
		{1000, "mg", "g", 1},
		{1, "[lb_av]", "kg", 0.45359237},
		{2, "km", "m", 2000},
		{500, "mL", "L", 0.5},
	}
	for _, tc := range cases {
		got, ok := conv.Convert(tc.value, tc.from, tc.to)
		if !ok {
			t.Fatalf("convert %v %s -> %s failed", tc.value, tc.from, tc.to)
		}
		if math.Abs(got-tc.want) > 1e-6*math.Max(1, math.Abs(tc.want)) {
			t.Fatalf("%v %s -> %s: got %v want %v", tc.value, tc.from, tc.to, got, tc.want)
		}
	}
}

func TestUCUMColloquialAliases(t *testing.T) {
	conv := DefaultUCUMConverter()
	got, ok := conv.Convert(12, "in", "cm")
	if !ok || math.Abs(got-30.48) > 0.01 {
		t.Fatalf("in -> cm: %v ok=%v", got, ok)
	}
}

func TestConvertQuantityUsesUCUM(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "ConvertQuantity(100 'mg', 'g')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	q, ok := asQuantity(got[0])
	if !ok || q.Value != 0.1 || q.Unit != "g" {
		t.Fatalf("ConvertQuantity: %#v", got)
	}
}
