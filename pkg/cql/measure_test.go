package cql

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func TestEvaluateMeasureIndividualAndSummary(t *testing.T) {
	eng, err := NewEngine(Config{
		Now: func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	lib, err := eng.ParseLibrary(`
library Adult version '1.0.0'
using FHIR version '4.0.1'
parameter "Measurement Period" Interval<DateTime>
context Patient
define "Initial Population":
  true
define "Denominator":
  AgeInYears() >= 18
define "Numerator":
  Patient.gender = 'female'
`)
	if err != nil {
		t.Fatal(err)
	}
	measure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
		"resourceType": "Measure",
		"id": "adult",
		"url": "http://example.org/Measure/Adult",
		"version": "1.0.0",
		"status": "active",
		"library": ["http://example.org/Library/Adult"],
		"scoring": {"coding": [{"code": "proportion"}]},
		"group": [{
			"population": [
				{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Initial Population"}},
				{"code": {"coding": [{"code": "denominator"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Denominator"}},
				{"code": {"coding": [{"code": "numerator"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Numerator"}}
			]
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	child, err := types.NewJSONCodec().ParseJSON("Patient", []byte(`{
		"resourceType": "Patient", "id": "kid", "gender": "male", "birthDate": "2020-01-01"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	periodStart := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)

	individual, err := eng.EvaluateMeasure(context.Background(), MeasureRequest{
		Measure:     measure,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		ReportType:  "individual",
		Patient:     adaPatient(t),
		Libraries:   []*Library{lib},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := decodeReport(t, individual)
	if report["type"] != "individual" {
		t.Fatalf("type: %#v", report["type"])
	}
	if score := measureScoreValue(t, report); score != 1 {
		t.Fatalf("individual score: %v", score)
	}

	summary, err := eng.EvaluateMeasure(context.Background(), MeasureRequest{
		Measure:     measure,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		ReportType:  "summary",
		Patients:    []any{adaPatient(t), child},
		Libraries:   []*Library{lib},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := decodeReport(t, summary)
	if sum["type"] != "summary" {
		t.Fatalf("summary type: %#v", sum["type"])
	}
	if score := measureScoreValue(t, sum); score != 1 {
		t.Fatalf("summary score (1 adult female / 1 adult): %v", score)
	}
	pops := populationCounts(t, sum)
	if pops["initial-population"] != 2 || pops["denominator"] != 1 || pops["numerator"] != 1 {
		t.Fatalf("summary counts: %#v", pops)
	}
}

func decodeReport(t *testing.T, env *types.ResourceEnvelope) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(env.JSON, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func measureScoreValue(t *testing.T, report map[string]any) float64 {
	t.Helper()
	groups, _ := report["group"].([]any)
	if len(groups) == 0 {
		t.Fatal("no groups")
	}
	g, _ := groups[0].(map[string]any)
	score, _ := g["measureScore"].(map[string]any)
	switch v := score["value"].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		t.Fatalf("score: %#v", score)
		return 0
	}
}

func populationCounts(t *testing.T, report map[string]any) map[string]int {
	t.Helper()
	out := map[string]int{}
	groups, _ := report["group"].([]any)
	g, _ := groups[0].(map[string]any)
	pops, _ := g["population"].([]any)
	for _, raw := range pops {
		p, _ := raw.(map[string]any)
		code, _ := p["code"].(map[string]any)
		coding, _ := code["coding"].([]any)
		c0, _ := coding[0].(map[string]any)
		name, _ := c0["code"].(string)
		switch n := p["count"].(type) {
		case float64:
			out[name] = int(n)
		case int:
			out[name] = n
		case json.Number:
			i, _ := n.Int64()
			out[name] = int(i)
		}
	}
	return out
}
