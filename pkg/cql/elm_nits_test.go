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

func TestELMToLongAndCQL(t *testing.T) {
	got := evalELMExpr(t, map[string]any{
		"type":    "ToLong",
		"operand": elmStrLit("42"),
	})
	if len(got) != 1 || got[0] != int64(42) {
		t.Fatalf("ToLong: %#v", got)
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "ToLong('7')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(7) {
		t.Fatalf("CQL ToLong: %#v", got)
	}
}

func TestELMAndCQLSuccessorPredecessor(t *testing.T) {
	got := evalELMExpr(t, map[string]any{"type": "Successor", "operand": elmIntLit("4")})
	if len(got) != 1 || got[0] != int64(5) {
		t.Fatalf("Successor: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Predecessor", "operand": elmIntLit("4")})
	if len(got) != 1 || got[0] != int64(3) {
		t.Fatalf("Predecessor: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":    "Successor",
		"operand": map[string]any{"type": "Date", "year": 2020, "month": 1, "day": 1},
	})
	tm, ok := asTime(got[0])
	if !ok || tm.Year() != 2020 || tm.Month() != time.January || tm.Day() != 2 {
		t.Fatalf("Successor date: %#v", got)
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "successor of 9", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(10) {
		t.Fatalf("CQL successor: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "predecessor of @2020-01-01", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	tm, ok = asTime(got[0])
	if !ok || tm.Day() != 31 || tm.Month() != time.December {
		t.Fatalf("CQL predecessor date: %#v", got)
	}
}

func TestELMSliceTailAndCQL(t *testing.T) {
	src := map[string]any{"type": "List", "element": []any{elmIntLit("1"), elmIntLit("2"), elmIntLit("3"), elmIntLit("4")}}
	got := evalELMExpr(t, map[string]any{
		"type":       "Slice",
		"source":     src,
		"startIndex": elmIntLit("1"),
		"endIndex":   elmIntLit("3"),
	})
	if len(got) != 2 || got[0] != int64(2) || got[1] != int64(3) {
		t.Fatalf("named Slice: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Tail", "source": src})
	if len(got) != 3 || got[0] != int64(2) || got[2] != int64(4) {
		t.Fatalf("Tail: %#v", got)
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Slice({1, 2, 3, 4}, 1, 3)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != int64(2) || got[1] != int64(3) {
		t.Fatalf("CQL Slice: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Tail({1, 2, 3})", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != int64(2) || got[1] != int64(3) {
		t.Fatalf("CQL Tail: %#v", got)
	}
}

func TestELMPowerLnLogExpAndCQLCaret(t *testing.T) {
	got := evalELMExpr(t, map[string]any{"type": "Power", "operand": []any{elmIntLit("2"), elmIntLit("3")}})
	if len(got) != 1 || got[0] != int64(8) {
		t.Fatalf("ELM Power: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Ln", "operand": map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Decimal", "value": "1"}})
	if len(got) != 1 || asFloatMust(t, got[0]) != 0 {
		t.Fatalf("Ln: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Exp", "operand": elmIntLit("0")})
	if len(got) != 1 || asFloatMust(t, got[0]) != 1 {
		t.Fatalf("Exp: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "Log", "operand": []any{
		map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Decimal", "value": "100"},
		map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Decimal", "value": "10"},
	}})
	if len(got) != 1 || asFloatMust(t, got[0]) != 2 {
		t.Fatalf("Log: %#v", got)
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "2 ^ 3", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(8) {
		t.Fatalf("CQL ^: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "2 ^ 3 ^ 2", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(512) {
		t.Fatalf("CQL ^ right-assoc 2^(3^2): %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Power(2, 3)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(8) {
		t.Fatalf("CQL Power: %#v", got)
	}
}

func TestELMCollapsePer(t *testing.T) {
	interval := func(low, high string) map[string]any {
		return map[string]any{"type": "Interval", "low": elmIntLit(low), "high": elmIntLit(high)}
	}
	got := evalELMExpr(t, map[string]any{
		"type":    "Collapse",
		"operand": map[string]any{"type": "List", "element": []any{interval("1", "2"), interval("4", "5")}},
		"per":     elmIntLit("1"),
	})
	if len(got) != 1 {
		t.Fatalf("collapse per: %#v", got)
	}
	iv, ok := asInterval(got[0])
	if !ok {
		t.Fatalf("collapse per not interval: %#v", got[0])
	}
	lo, _ := asInt(iv.Low)
	hi, _ := asInt(iv.High)
	if lo != 1 || hi != 5 {
		t.Fatalf("collapse per bounds: %#v", got[0])
	}
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "collapse { Interval[1, 2], Interval[4, 5] } per 1", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("CQL collapse per: %#v", got)
	}
	iv, ok = asInterval(got[0])
	lo, _ = asInt(iv.Low)
	hi, _ = asInt(iv.High)
	if !ok || lo != 1 || hi != 5 {
		t.Fatalf("CQL collapse per bounds: %#v", got)
	}
}

func TestELMAnyAllInCodeSystem(t *testing.T) {
	n, err := parseELMExpr(map[string]any{
		"type": "AnyInCodeSystem",
		"codes": map[string]any{"type": "List", "element": []any{
			map[string]any{"type": "Code", "code": "a", "system": "http://example.org/cs"},
		}},
		"codesystem": map[string]any{"type": "CodeSystemRef", "name": "CS"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, ok := n.(*binaryNode)
	if !ok || b.op != "any in" {
		t.Fatalf("AnyInCodeSystem: %#v", n)
	}
	n, err = parseELMExpr(map[string]any{
		"type":       "AllInCodeSystem",
		"code":       map[string]any{"type": "Code", "code": "a", "system": "http://example.org/cs"},
		"codesystem": map[string]any{"type": "CodeSystemRef", "name": "CS"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, ok = n.(*binaryNode)
	if !ok || b.op != "all in" {
		t.Fatalf("AllInCodeSystem: %#v", n)
	}
	eng := testEngine(t)
	lib, err := eng.ParseLibrary(`
library CSLib version '1.0.0'
codesystem "CS": 'http://example.org/cs'
define "InCS": { system: 'http://example.org/cs', code: 'a' } in "CS"
define "NotInCS": { system: 'http://other.org', code: 'a' } in "CS"
`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "InCS", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("code in codesystem: %#v", got)
	}
	got, err = eng.EvalDefine(context.Background(), lib, "NotInCS", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("code not in codesystem: %#v", got)
	}
}

func TestELMConceptAndRetrieveListKeepAllCodes(t *testing.T) {
	n, err := parseELMExpr(map[string]any{
		"type": "Concept",
		"codes": []any{
			map[string]any{"type": "Code", "code": "8867-4", "system": "http://loinc.org"},
			map[string]any{"type": "Code", "code": "8480-6", "system": "http://loinc.org"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := n.(*listNode)
	if !ok || len(list.elems) != 2 {
		t.Fatalf("Concept codes: %#v", n)
	}
	got := evalELMExpr(t, map[string]any{
		"type": "Concept",
		"codes": []any{
			map[string]any{"type": "Code", "code": "8867-4", "system": "http://loinc.org"},
			map[string]any{"type": "Code", "code": "8480-6", "system": "http://loinc.org"},
		},
	})
	if len(got) != 2 {
		t.Fatalf("Concept eval: %#v", got)
	}
	c0, ok := got[0].(Code)
	c1, ok1 := got[1].(Code)
	if !ok || !ok1 || c0.Code != "8867-4" || c1.Code != "8480-6" {
		t.Fatalf("Concept values: %#v", got)
	}
	r, err := parseELMExpr(map[string]any{
		"type":     "Retrieve",
		"dataType": "{http://hl7.org/fhir}Observation",
		"codes": map[string]any{
			"type": "List",
			"element": []any{
				map[string]any{"type": "Code", "code": "8867-4", "system": "http://loinc.org"},
				map[string]any{"type": "Code", "code": "8480-6", "system": "http://loinc.org"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ret, ok := r.(*retrieveNode)
	if !ok || ret.terminology != "http://loinc.org|8867-4;http://loinc.org|8480-6" {
		t.Fatalf("retrieve list codes: %+v", r)
	}
	codes := []fhirCoding{
		{System: "http://loinc.org", Code: "8480-6"},
	}
	if !codingMatchesExact(codes, "", "", "http://loinc.org|8867-4;http://loinc.org|8480-6") {
		t.Fatal("multi-term retrieve should match any listed code")
	}
}

func TestExpandPerInteger(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "expand Interval[1, 10] per 2", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("expand per 2: %#v", got)
	}
	first, ok := asInterval(got[0])
	last, ok2 := asInterval(got[4])
	if !ok || !ok2 || first.Low != int64(1) || last.Low != int64(9) {
		t.Fatalf("expand per 2 bounds: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":    "Expand",
		"operand": map[string]any{"type": "Interval", "low": elmIntLit("1"), "high": elmIntLit("10")},
		"per":     elmIntLit("2"),
	})
	if len(got) != 5 {
		t.Fatalf("ELM expand per 2: %#v", got)
	}
}

func TestConceptIdentLookup(t *testing.T) {
	eng := testEngine(t)
	lib, err := eng.ParseLibrary(`
library Probe version '1.0.0'
codesystem "CS": 'http://example.org/cs'
code "A": 'a' from "CS"
code "B": 'b' from "CS"
concept "Both": { "A", "B" }
define "X": "Both"
`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := eng.EvalDefine(context.Background(), lib, "X", EvalContext{Libraries: []*Library{lib}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("concept ident: %#v", got)
	}
	c0, ok := got[0].(Code)
	c1, ok1 := got[1].(Code)
	if !ok || !ok1 || c0.Code != "a" || c1.Code != "b" {
		t.Fatalf("concept codes: %#v", got)
	}
	n, err := parseELMExpr(map[string]any{
		"type":     "Retrieve",
		"dataType": "{http://hl7.org/fhir}Observation",
		"codes":    map[string]any{"type": "ConceptRef", "name": "Both"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ret, ok := n.(*retrieveNode)
	if !ok || ret.terminology != "Both" {
		t.Fatalf("ConceptRef retrieve: %+v", n)
	}
}

func TestToQuantityIntegerAndString(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "ToQuantity(5)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	q, ok := asQuantity(got[0])
	if !ok || q.Value != 5 || q.Unit != "1" {
		t.Fatalf("ToQuantity(5): %#v", got)
	}
	got, err = eng.Eval(context.Background(), "ToQuantity('5 mg')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	q, ok = asQuantity(got[0])
	if !ok || q.Value != 5 || q.Unit != "mg" {
		t.Fatalf("ToQuantity string: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "ToQuantity('5.5 ''cm''')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	q, ok = asQuantity(got[0])
	if !ok || q.Value != 5.5 || q.Unit != "cm" {
		t.Fatalf("ToQuantity quoted unit: %#v", got)
	}
}

func TestToConcept(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "ToConcept({code: 'a', system: 'http://example.org'})", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := got[0].(Code)
	if !ok || c.Code != "a" || c.System != "http://example.org" {
		t.Fatalf("ToConcept: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":    "ToConcept",
		"operand": map[string]any{"type": "Code", "code": "a", "system": "http://example.org"},
	})
	c, ok = got[0].(Code)
	if !ok || c.Code != "a" {
		t.Fatalf("ELM ToConcept: %#v", got)
	}
}

func TestELMLogNamedBase(t *testing.T) {
	got := evalELMExpr(t, map[string]any{
		"type":    "Log",
		"operand": elmIntLit("8"),
		"base":    elmIntLit("2"),
	})
	if asFloatMust(t, got[0]) != 3 {
		t.Fatalf("Log named base: %#v", got)
	}
}

func TestPointFromAndBoundaries(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "point from Interval[1, 1]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("point from: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "point from Interval[1, 2]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("point from non-unit: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "high boundary of Interval[1, 10]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(10) {
		t.Fatalf("high boundary: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "low boundary of Interval[1, 10]", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("low boundary: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "precision of 1.23", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(2) {
		t.Fatalf("precision of: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":    "PointFrom",
		"operand": map[string]any{"type": "Interval", "low": elmIntLit("1"), "high": elmIntLit("1")},
	})
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("ELM PointFrom: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type":      "HighBoundary",
		"operand":   map[string]any{"type": "Literal", "valueType": "{urn:hl7-org:elm-types:r1}Decimal", "value": "1.587"},
		"precision": 8,
	})
	if asFloatMust(t, got[0]) != 1.587 {
		t.Fatalf("HighBoundary decimal: %#v", got)
	}
}

func TestListStatsAndProduct(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Median({1, 3, 5})", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(3) {
		t.Fatalf("Median: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Mode({1, 1, 2})", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("Mode: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "StdDev({1.0, 2.0, 3.0})", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if asFloatMust(t, got[0]) != 1 {
		t.Fatalf("StdDev: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Variance({1.0, 2.0, 3.0})", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if asFloatMust(t, got[0]) != 1 {
		t.Fatalf("Variance: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Product({2, 3, 4})", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(24) {
		t.Fatalf("Product: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "GeometricMean({1.0, 4.0})", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if asFloatMust(t, got[0]) != 2 {
		t.Fatalf("GeometricMean: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{
		"type": "Median",
		"source": map[string]any{
			"type":    "List",
			"element": []any{elmIntLit("1"), elmIntLit("3"), elmIntLit("5")},
		},
	})
	if len(got) != 1 || got[0] != int64(3) {
		t.Fatalf("ELM Median: %#v", got)
	}
}

func TestConvertsToAndToChars(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "ConvertsToInteger('5')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("ConvertsToInteger: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "ConvertsToInteger('nope')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("ConvertsToInteger false: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "ConvertsToBoolean('true')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("ConvertsToBoolean: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "CanConvertQuantity(5 'mg', 'g')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("CanConvertQuantity: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "CanConvertQuantity(5 'mg', 'cm')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("CanConvertQuantity false: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "ToChars('ab')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ToChars: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "ToChars", "operand": elmStrLit("ab")})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ELM ToChars: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "ConvertsToInteger", "operand": elmStrLit("5")})
	if len(got) != 1 || got[0] != true {
		t.Fatalf("ELM ConvertsToInteger: %#v", got)
	}
}

func TestMinMaxValue(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "maximum Integer", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(2147483647) {
		t.Fatalf("maximum Integer: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "minimum Integer", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(-2147483648) {
		t.Fatalf("minimum Integer: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "MaxValue", "valueType": "{urn:hl7-org:elm-types:r1}Integer"})
	if len(got) != 1 || got[0] != int64(2147483647) {
		t.Fatalf("ELM MaxValue: %#v", got)
	}
	got = evalELMExpr(t, map[string]any{"type": "MinValue", "valueType": "{urn:hl7-org:elm-types:r1}Integer"})
	if len(got) != 1 || got[0] != int64(-2147483648) {
		t.Fatalf("ELM MinValue: %#v", got)
	}
}

func TestCQLAllAnyIn(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "{1, 2} all in {1, 2, 3}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("all in: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{1, 9} any in {1, 2, 3}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("any in: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "{8, 9} any in {1, 2, 3}", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("any in false: %#v", got)
	}
}

func TestToBooleanSpec(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "ToBoolean(1)", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("ToBoolean(1): %#v", got)
	}
	got, err = eng.Eval(context.Background(), "ToBoolean('1')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != true {
		t.Fatalf("ToBoolean('1'): %#v", got)
	}
	got, err = eng.Eval(context.Background(), "ToBoolean('n')", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != false {
		t.Fatalf("ToBoolean('n'): %#v", got)
	}
}

func TestChildrenDescendants(t *testing.T) {
	eng := testEngine(t)
	got, err := eng.Eval(context.Background(), "Children({a: 1})", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != int64(1) {
		t.Fatalf("Children: %#v", got)
	}
	got, err = eng.Eval(context.Background(), "Descendants({a: {b: 1}})", EvalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Descendants: %#v", got)
	}
	nested, ok := asObject(got[0])
	if !ok || nested["b"] != int64(1) || got[1] != int64(1) {
		t.Fatalf("Descendants values: %#v", got)
	}
}
