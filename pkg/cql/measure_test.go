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

func TestEvaluateMeasureListMembershipAndStratifierAndSDE(t *testing.T) {
	obs1, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation", "id": "o1", "status": "final",
		"code": {"text": "Heart rate"}, "subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	obs2, err := types.NewJSONCodec().ParseJSON("Observation", []byte(`{
		"resourceType": "Observation", "id": "o2", "status": "final",
		"code": {"text": "Heart rate"}, "subject": {"reference": "Patient/ada"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := NewEngine(Config{
		Retriever: StaticRetriever{obs1, obs2},
		Now:       func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
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
  AgeInYears() >= 18
define "Observed":
  [Observation]
define "Denominator":
  AgeInYears() >= 18
define "Numerator":
  Patient.gender = 'female'
define "On Period End":
  @2021-01-01 during "Measurement Period"
define "On Period End Day":
  @2021-01-01T10:00:00 during "Measurement Period"
define "SDE Sex":
  Patient.gender
`)
	if err != nil {
		t.Fatal(err)
	}
	listMeasure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
		"resourceType": "Measure",
		"id": "obs",
		"url": "http://example.org/Measure/Obs",
		"library": ["http://example.org/Library/Adult"],
		"scoring": {"coding": [{"code": "cohort"}]},
		"supplementalData": [{
			"id": "sde-sex",
			"code": {"coding": [{"code": "SEX"}]},
			"criteria": {"language": "text/cql.identifier", "expression": "SDE Sex"}
		}],
		"group": [{
			"population": [
				{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Observed"}}
			]
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	measure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
		"resourceType": "Measure",
		"id": "adult",
		"url": "http://example.org/Measure/Adult",
		"library": ["http://example.org/Library/Adult"],
		"scoring": {"coding": [{"code": "proportion"}]},
		"supplementalData": [{
			"id": "sde-sex",
			"code": {"coding": [{"code": "SEX"}]},
			"criteria": {"language": "text/cql.identifier", "expression": "SDE Sex"}
		}],
		"group": [{
			"population": [
				{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Initial Population"}},
				{"code": {"coding": [{"code": "denominator"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Denominator"}},
				{"code": {"coding": [{"code": "numerator"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Numerator"}}
			],
			"stratifier": [{
				"code": {"coding": [{"code": "gender"}]},
				"criteria": {"language": "text/cql", "expression": "Patient.gender"}
			}]
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
		Measure:     listMeasure,
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
	pops := populationCounts(t, report)
	if pops["initial-population"] != 1 {
		t.Fatalf("individual list criteria must count membership 0/1, got %#v", pops)
	}
	contained, _ := report["contained"].([]any)
	if len(contained) == 0 {
		t.Fatalf("supplemental data missing from MeasureReport: %#v", report)
	}
	sde, _ := contained[0].(map[string]any)
	if sde["valueString"] != "female" {
		t.Fatalf("supplemental data value: %#v", sde)
	}

	periodMeasure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
		"resourceType": "Measure",
		"id": "period",
		"url": "http://example.org/Measure/Period",
		"library": ["http://example.org/Library/Adult"],
		"scoring": {"coding": [{"code": "cohort"}]},
		"group": [{"population": [
			{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "On Period End"}}
		]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	periodReport, err := eng.EvaluateMeasure(context.Background(), MeasureRequest{
		Measure:     periodMeasure,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		ReportType:  "individual",
		Patient:     adaPatient(t),
		Libraries:   []*Library{lib},
	})
	if err != nil {
		t.Fatal(err)
	}
	if populationCounts(t, decodeReport(t, periodReport))["initial-population"] != 1 {
		t.Fatalf("event on periodEnd must be inside closed Measurement Period: %#v", decodeReport(t, periodReport))
	}

	periodDayMeasure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
		"resourceType": "Measure",
		"id": "period-day",
		"url": "http://example.org/Measure/PeriodDay",
		"library": ["http://example.org/Library/Adult"],
		"scoring": {"coding": [{"code": "cohort"}]},
		"group": [{"population": [
			{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "On Period End Day"}}
		]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	periodDayReport, err := eng.EvaluateMeasure(context.Background(), MeasureRequest{
		Measure:     periodDayMeasure,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		ReportType:  "individual",
		Patient:     adaPatient(t),
		Libraries:   []*Library{lib},
	})
	if err != nil {
		t.Fatal(err)
	}
	if populationCounts(t, decodeReport(t, periodDayReport))["initial-population"] != 1 {
		t.Fatalf("datetime on periodEnd date must be inside date-only period: %#v", decodeReport(t, periodDayReport))
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
	if n := stratifierIPCount(t, sum); n != 1 {
		t.Fatalf("stratifier should count initial population only, got %d in %#v", n, sum)
	}
	if n := stratifierPopCount(t, sum, "denominator"); n != 1 {
		t.Fatalf("stratifier denominator should be 1, got %d in %#v", n, sum)
	}
	contained, _ = sum["contained"].([]any)
	if len(contained) != 1 {
		t.Fatalf("supplemental data should only include initial population, got %#v", contained)
	}
}

func stratifierIPCount(t *testing.T, report map[string]any) int {
	t.Helper()
	groups, _ := report["group"].([]any)
	g, _ := groups[0].(map[string]any)
	strats, _ := g["stratifier"].([]any)
	if len(strats) == 0 {
		return 0
	}
	s, _ := strats[0].(map[string]any)
	strata, _ := s["stratum"].([]any)
	total := 0
	for _, raw := range strata {
		st, _ := raw.(map[string]any)
		pops, _ := st["population"].([]any)
		if len(pops) == 0 {
			continue
		}
		p, _ := pops[0].(map[string]any)
		switch n := p["count"].(type) {
		case float64:
			total += int(n)
		case int:
			total += n
		}
	}
	return total
}

func stratifierPopCount(t *testing.T, report map[string]any, popCode string) int {
	t.Helper()
	groups, _ := report["group"].([]any)
	g, _ := groups[0].(map[string]any)
	strats, _ := g["stratifier"].([]any)
	if len(strats) == 0 {
		return 0
	}
	s, _ := strats[0].(map[string]any)
	strata, _ := s["stratum"].([]any)
	total := 0
	for _, raw := range strata {
		st, _ := raw.(map[string]any)
		pops, _ := st["population"].([]any)
		for _, praw := range pops {
			p, _ := praw.(map[string]any)
			code, _ := p["code"].(map[string]any)
			coding, _ := code["coding"].([]any)
			c0, _ := coding[0].(map[string]any)
			if c0["code"] != popCode {
				continue
			}
			switch n := p["count"].(type) {
			case float64:
				total += int(n)
			case int:
				total += n
			}
		}
	}
	return total
}

func TestEvaluateMeasureUsesPeriodEndClockAndRawExclusionCounts(t *testing.T) {
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
define "Initial Population": true
define "Denominator": AgeInYears() >= 18
define "Denominator Exclusion": true
define "Numerator": false
`)
	if err != nil {
		t.Fatal(err)
	}
	measure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
		"resourceType": "Measure",
		"id": "adult",
		"url": "http://example.org/Measure/Adult",
		"scoring": {"coding": [{"code": "proportion"}]},
		"group": [{"population": [
			{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Initial Population"}},
			{"code": {"coding": [{"code": "denominator"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Denominator"}},
			{"code": {"coding": [{"code": "denominator-exclusion"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Denominator Exclusion"}},
			{"code": {"coding": [{"code": "numerator"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Numerator"}}
		]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	youngPeriod := time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
	young, err := eng.EvaluateMeasure(context.Background(), MeasureRequest{
		Measure:     measure,
		PeriodStart: time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   youngPeriod,
		ReportType:  "individual",
		Patient:     adaPatient(t),
		Libraries:   []*Library{lib},
	})
	if err != nil {
		t.Fatal(err)
	}
	pops := populationCounts(t, decodeReport(t, young))
	if pops["denominator"] != 0 {
		t.Fatalf("AgeInYears must use period end (2017), not engine Now (2026): %#v", pops)
	}

	adultPeriodEnd := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	adult, err := eng.EvaluateMeasure(context.Background(), MeasureRequest{
		Measure:     measure,
		PeriodStart: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   adultPeriodEnd,
		ReportType:  "individual",
		Patient:     adaPatient(t),
		Libraries:   []*Library{lib},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := decodeReport(t, adult)
	pops = populationCounts(t, report)
	if pops["denominator"] != 1 || pops["denominator-exclusion"] != 1 {
		t.Fatalf("raw population counts should keep exclusions separate: %#v", pops)
	}
	groups, _ := report["group"].([]any)
	g, _ := groups[0].(map[string]any)
	if score, ok := g["measureScore"].(map[string]any); ok {
		if measureScoreValue(t, report) != 0 {
			t.Fatalf("score after exclusion should be 0, got %#v", score)
		}
	}
}

func TestClosedPeriodEndExpandsLocalMidnight(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	got := closedPeriodEnd(time.Date(2021, 1, 1, 0, 0, 0, 0, loc))
	want := time.Date(2021, 1, 1, 23, 59, 59, 999999999, loc)
	if !got.Equal(want) || got.Location().String() != loc.String() {
		t.Fatalf("local midnight: got %v want %v", got, want)
	}
	got = closedPeriodEnd(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC))
	want = time.Date(2021, 1, 1, 23, 59, 59, 999999999, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("utc midnight: got %v want %v", got, want)
	}
	midday := time.Date(2021, 1, 1, 15, 4, 5, 0, loc)
	if !closedPeriodEnd(midday).Equal(midday) {
		t.Fatalf("non-midnight must stay put: %v", closedPeriodEnd(midday))
	}
}

func TestEvaluateMeasureIPGatesDenominatorAndNumerator(t *testing.T) {
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
  AgeInYears() >= 18
define "Denominator":
  true
define "Numerator":
  true
`)
	if err != nil {
		t.Fatal(err)
	}
	measure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
		"resourceType": "Measure",
		"id": "adult",
		"url": "http://example.org/Measure/Adult",
		"scoring": {"coding": [{"code": "proportion"}]},
		"group": [{"population": [
			{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Initial Population"}},
			{"code": {"coding": [{"code": "denominator"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Denominator"}},
			{"code": {"coding": [{"code": "numerator"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Numerator"}}
		]}]
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
	report, err := eng.EvaluateMeasure(context.Background(), MeasureRequest{
		Measure:     measure,
		PeriodStart: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
		ReportType:  "subject-list",
		Patients:    []any{adaPatient(t), child},
		Libraries:   []*Library{lib},
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeReport(t, report)
	pops := populationCounts(t, decoded)
	if pops["initial-population"] != 1 || pops["denominator"] != 1 || pops["numerator"] != 1 {
		t.Fatalf("populations outside initial-population must not count: %#v", pops)
	}
	contained, _ := decoded["contained"].([]any)
	for _, raw := range contained {
		list, _ := raw.(map[string]any)
		entries, _ := list["entry"].([]any)
		for _, eraw := range entries {
			e, _ := eraw.(map[string]any)
			item, _ := e["item"].(map[string]any)
			if item["reference"] == "Patient/kid" {
				t.Fatalf("subject-list must not include patients outside IP: %#v", decoded)
			}
		}
	}
}

func TestEvaluateMeasurePopulationNullAndFalseLists(t *testing.T) {
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
define "Null Pop":
  from {1} X return null
define "False List":
  {false, false}
define "True List":
  {false, true}
`)
	if err != nil {
		t.Fatal(err)
	}
	eval := func(expr string) int {
		t.Helper()
		measure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
			"resourceType": "Measure",
			"id": "pop",
			"url": "http://example.org/Measure/Pop",
			"scoring": {"coding": [{"code": "cohort"}]},
			"group": [{"population": [
				{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "`+expr+`"}}
			]}]
		}`))
		if err != nil {
			t.Fatal(err)
		}
		report, err := eng.EvaluateMeasure(context.Background(), MeasureRequest{
			Measure:     measure,
			PeriodStart: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
			ReportType:  "individual",
			Patient:     adaPatient(t),
			Libraries:   []*Library{lib},
		})
		if err != nil {
			t.Fatal(err)
		}
		return populationCounts(t, decodeReport(t, report))["initial-population"]
	}
	if n := eval("Null Pop"); n != 0 {
		t.Fatalf("singleton null population: %d", n)
	}
	if n := eval("False List"); n != 0 {
		t.Fatalf("{false, false} population: %d", n)
	}
	if n := eval("True List"); n != 1 {
		t.Fatalf("{false, true} population: %d", n)
	}
}

func TestEvaluateMeasureSDEIncludesAnyGroupIP(t *testing.T) {
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
define "Adult":
  AgeInYears() >= 18
define "Male":
  Patient.gender = 'male'
define "SDE Sex":
  Patient.gender
`)
	if err != nil {
		t.Fatal(err)
	}
	measure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
		"resourceType": "Measure",
		"id": "adult",
		"url": "http://example.org/Measure/Adult",
		"scoring": {"coding": [{"code": "cohort"}]},
		"supplementalData": [{
			"id": "sde-sex",
			"code": {"coding": [{"code": "SEX"}]},
			"criteria": {"language": "text/cql.identifier", "expression": "SDE Sex"}
		}],
		"group": [
			{"population": [
				{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Adult"}}
			]},
			{"population": [
				{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Male"}}
			]}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	report, err := eng.EvaluateMeasure(context.Background(), MeasureRequest{
		Measure:     measure,
		PeriodStart: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
		ReportType:  "individual",
		Patient:     adaPatient(t),
		Libraries:   []*Library{lib},
	})
	if err != nil {
		t.Fatal(err)
	}
	contained, _ := decodeReport(t, report)["contained"].([]any)
	if len(contained) != 1 {
		t.Fatalf("SDE must include subjects in any group initial-population, got %#v", contained)
	}
}

func TestMeasureReportPeriodUsesLocalDate(t *testing.T) {
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
define "Initial Population": true
`)
	if err != nil {
		t.Fatal(err)
	}
	measure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
		"resourceType": "Measure",
		"id": "adult",
		"scoring": {"coding": [{"code": "cohort"}]},
		"group": [{"population": [
			{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Initial Population"}}
		]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("EST", -5*3600)
	report, err := eng.EvaluateMeasure(context.Background(), MeasureRequest{
		Measure:     measure,
		PeriodStart: time.Date(2020, 1, 1, 0, 0, 0, 0, loc),
		PeriodEnd:   time.Date(2021, 1, 1, 0, 0, 0, 0, loc),
		ReportType:  "individual",
		Patient:     adaPatient(t),
		Libraries:   []*Library{lib},
	})
	if err != nil {
		t.Fatal(err)
	}
	period, _ := decodeReport(t, report)["period"].(map[string]any)
	if period["start"] != "2020-01-01" || period["end"] != "2021-01-01" {
		t.Fatalf("MeasureReport.period must use local calendar dates: %#v", period)
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
