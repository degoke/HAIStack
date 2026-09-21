package cql

import (
	"context"
	"testing"
	"time"
)

func elmStrLit(v string) map[string]any {
	return map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}String", "value": v}
}

func TestELMQueryLetRef(t *testing.T) {
	got := evalELMExpr(t, map[string]any{
		"type": "Query",
		"source": []any{map[string]any{
			"alias": "X",
			"expression": map[string]any{
				"type": "List",
				"element": []any{
					elmIntLit("10"),
					elmIntLit("20"),
				},
			},
		}},
		"let": []any{map[string]any{
			"identifier": "d",
			"expression": map[string]any{"type": "AliasRef", "name": "X"},
		}},
		"return": map[string]any{
			"expression": map[string]any{"type": "QueryLetRef", "name": "d"},
		},
	})
	if len(got) != 2 || got[0] != int64(10) || got[1] != int64(20) {
		t.Fatalf("QueryLetRef: %#v", got)
	}
}

func TestELMAndCQLDateFrom(t *testing.T) {
	got := evalELMExpr(t, map[string]any{
		"type": "DateFrom",
		"operand": map[string]any{
			"type": "DateTime", "year": 2021, "month": 6, "day": 15,
			"hour": 14, "minute": 30, "second": 0, "millisecond": 0,
		},
	})
	tm, ok := asTime(got[0])
	if !ok || tm.Year() != 2021 || tm.Month() != time.June || tm.Day() != 15 || tm.Hour() != 0 {
		t.Fatalf("DateFrom: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":      "DateTimeComponentFrom",
		"precision": "Year",
		"operand": map[string]any{
			"type": "DateTime", "year": 2021, "month": 6, "day": 15,
			"hour": 14, "minute": 30, "second": 0, "millisecond": 0,
		},
	})
	if len(got) != 1 || got[0] != int64(2021) {
		t.Fatalf("year from DateTime: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type": "TimeFrom",
		"operand": map[string]any{
			"type": "DateTime", "year": 2021, "month": 6, "day": 15,
			"hour": 14, "minute": 30, "second": 45, "millisecond": 0,
		},
	})
	tm, ok = asTime(got[0])
	if !ok || tm.Year() != 2026 || tm.Hour() != 14 || tm.Minute() != 30 || tm.Second() != 45 {
		t.Fatalf("TimeFrom: %v", tm)
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "year from DateTime(2021, 6, 15, 14, 30, 0, 0)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(2021) {
		t.Fatalf("CQL year from: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "date from DateTime(2021, 6, 15, 14, 30, 0, 0)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	tm, ok = asTime(got[0])
	if !ok || tm.Year() != 2021 || tm.Day() != 15 || tm.Hour() != 0 {
		t.Fatalf("CQL date from: %v", tm)
	}
}

func TestELMNamedCallFields(t *testing.T) {
	got := evalELMExpr(t, map[string]any{
		"type":          "Split",
		"stringToSplit": elmStrLit("a,b,c"),
		"separator":     elmStrLit(","),
	})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("named Split: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":        "Substring",
		"stringToSub": elmStrLit("hello"),
		"startIndex":  elmIntLit("1"),
		"length":      elmIntLit("3"),
	})
	if len(got) != 1 || got[0] != "ell" {
		t.Fatalf("named Substring: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":      "Combine",
		"source":    map[string]any{"type": "List", "element": []any{elmStrLit("a"), elmStrLit("b")}},
		"separator": elmStrLit("-"),
	})
	if len(got) != 1 || got[0] != "a-b" {
		t.Fatalf("named Combine: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":    "IndexOf",
		"source":  map[string]any{"type": "List", "element": []any{elmIntLit("1"), elmIntLit("2"), elmIntLit("3")}},
		"element": elmIntLit("2"),
	})
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("named IndexOf: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":    "Take",
		"source":  map[string]any{"type": "List", "element": []any{elmIntLit("1"), elmIntLit("2"), elmIntLit("3")}},
		"element": elmIntLit("2"),
	})
	if len(got) != 2 || got[0] != int64(1) || got[1] != int64(2) {
		t.Fatalf("named Take: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":       "Skip",
		"source":     map[string]any{"type": "List", "element": []any{elmIntLit("1"), elmIntLit("2"), elmIntLit("3")}},
		"startIndex": elmIntLit("1"),
	})
	if len(got) != 2 || got[0] != int64(2) || got[1] != int64(3) {
		t.Fatalf("named Skip: %#v", got)
	}
}

func TestELMMathAndCQLRound(t *testing.T) {
	got := evalELMExpr(t, map[string]any{"type": "Round", "operand": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Decimal", "value": "1.5"}})
	if len(got) != 1 || got[0] != int64(2) {
		t.Fatalf("Round: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Abs", "operand": elmIntLit("-3")})
	if len(got) != 1 || asFloatMust(t, got[0]) != 3 {
		t.Fatalf("Abs: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Floor", "operand": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Decimal", "value": "1.9"}})
	if len(got) != 1 || asFloatMust(t, got[0]) != 1 {
		t.Fatalf("Floor: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Ceiling", "operand": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Decimal", "value": "1.1"}})
	if len(got) != 1 || asFloatMust(t, got[0]) != 2 {
		t.Fatalf("Ceiling: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Truncate", "operand": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Decimal", "value": "-1.9"}})
	if len(got) != 1 || asFloatMust(t, got[0]) != -1 {
		t.Fatalf("Truncate: %#v", got)
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Round(1.2)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("CQL Round: %#v", got)
	}
}

func asFloatMust(t *testing.T, v any) float64 {
	t.Helper()
	f, ok := asFloat(v)
	if !ok {
		t.Fatalf("not a number: %#v", v)
	}
	return f
}

func TestELMAnyAllInValueSet(t *testing.T) {
	n, err := parseELMExpr(map[string]any{
		"type":  "AnyInValueSet",
		"codes": map[string]any{"type": "List", "element": []any{elmIntLit("1"), elmIntLit("9")}},
		"valueset": map[string]any{
			"type":    "List",
			"element": []any{elmIntLit("1"), elmIntLit("2"), elmIntLit("3")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, ok := n.(*binaryNode)
	if !ok || b.op != "any in" {
		t.Fatalf("AnyInValueSet: %#v", n)
	}
	got := evalELMExpr(t, map[string]any{
		"type":  "AnyInValueSet",
		"codes": map[string]any{"type": "List", "element": []any{elmIntLit("1"), elmIntLit("9")}},
		"valueset": map[string]any{
			"type":    "List",
			"element": []any{elmIntLit("1"), elmIntLit("2"), elmIntLit("3")},
		},
	})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("AnyInValueSet eval: %#v", got)
	}
	n, err = parseELMExpr(map[string]any{
		"type":  "AllInValueSet",
		"codes": map[string]any{"type": "List", "element": []any{elmIntLit("1"), elmIntLit("9")}},
		"valueset": map[string]any{
			"type":    "List",
			"element": []any{elmIntLit("1"), elmIntLit("2"), elmIntLit("3")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, ok = n.(*binaryNode)
	if !ok || b.op != "all in" {
		t.Fatalf("AllInValueSet: %#v", n)
	}
	got = evalELMExpr(t, map[string]any{
		"type":  "AllInValueSet",
		"codes": map[string]any{"type": "List", "element": []any{elmIntLit("1"), elmIntLit("9")}},
		"valueset": map[string]any{
			"type":    "List",
			"element": []any{elmIntLit("1"), elmIntLit("2"), elmIntLit("3")},
		},
	})
	if len(got) != 1 || got[0] != false {
		t.Fatalf("AllInValueSet eval: %#v", got)
	}
}

func TestELMNowTodayTimezoneOffset(t *testing.T) {
	got := evalELMExpr(t, map[string]any{"type": "Now", "timezoneOffset": -5})
	if len(got) != 1 {
		t.Fatalf("Now offset: %#v", got)
	}
	tm, ok := asTime(got[0])
	if !ok {
		t.Fatalf("Now not a time: %#v", got[0])
	}
	_, off := tm.Zone()
	if off != -5*3600 {
		t.Fatalf("Now timezoneOffset ignored: %v off=%d", tm, off)
	}
	got = evalELMExpr(t, map[string]any{"type": "Today", "timezoneOffset": -5})
	tm, ok = asTime(got[0])
	if !ok {
		t.Fatalf("Today offset: %#v", got)
	}
	_, off = tm.Zone()
	if off != -5*3600 || tm.Hour() != 0 {
		t.Fatalf("Today timezoneOffset: %v off=%d", tm, off)
	}
	got = evalELMExpr(t, map[string]any{"type": "TimeOfDay", "timezoneOffset": -5})
	tm, ok = asTime(got[0])
	if !ok {
		t.Fatalf("TimeOfDay: %#v", got)
	}
	_, off = tm.Zone()
	if off != -5*3600 {
		t.Fatalf("TimeOfDay timezoneOffset ignored: %v off=%d", tm, off)
	}
}

func TestELMPositionReplaceConvertQuantity(t *testing.T) {
	got := evalELMExpr(t, map[string]any{
		"type":    "PositionOf",
		"pattern": elmStrLit("bc"),
		"string":  elmStrLit("abcde"),
	})
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("PositionOf: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":    "LastPositionOf",
		"pattern": elmStrLit("a"),
		"string":  elmStrLit("abca"),
	})
	if len(got) != 1 || got[0] != int64(3) {
		t.Fatalf("LastPositionOf: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":         "ReplaceMatches",
		"operand":      elmStrLit("a1b2"),
		"pattern":      elmStrLit(`\d`),
		"substitution": elmStrLit("-"),
	})
	if len(got) != 1 || got[0] != "a-b-" {
		t.Fatalf("ReplaceMatches: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type": "ConvertQuantity",
		"operand": []any{
			map[string]any{"type": "Quantity", "value": 1000, "unit": "mg"},
			elmStrLit("g"),
		},
	})
	if len(got) != 1 {
		t.Fatalf("ConvertQuantity: %#v", got)
	}
	q, ok := asQuantity(got[0])
	if !ok || q.Value != 1 || q.Unit != "g" {
		t.Fatalf("ConvertQuantity value: %#v", got[0])
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "ConvertQuantity(1 'g', 'mg')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	q, ok = asQuantity(got[0])
	if !ok || q.Value != 1000 || q.Unit != "mg" {
		t.Fatalf("CQL ConvertQuantity: %#v", got)
	}
}
