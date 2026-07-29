package search_test

import (
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/search"
)

func TestParseQueryRepeatedAndCommaOR(t *testing.T) {
	q, err := search.ParseQueryValues("Patient", map[string][]string{
		"name":   {"Smith,Jones", "Brown"},
		"_count": {"10"},
	})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	var nameClauses []search.ParamClause
	for _, p := range q.Params {
		if p.Code == "name" {
			nameClauses = append(nameClauses, p)
		}
	}
	if len(nameClauses) != 2 {
		t.Fatalf("name params = %d, want 2 AND clauses", len(nameClauses))
	}
	if len(nameClauses[0].Values) != 2 || nameClauses[0].Values[0].Raw != "Smith" || nameClauses[0].Values[1].Raw != "Jones" {
		t.Fatalf("first name clause = %#v", nameClauses[0].Values)
	}
	if len(nameClauses[1].Values) != 1 || nameClauses[1].Values[0].Raw != "Brown" {
		t.Fatalf("second name clause = %#v", nameClauses[1].Values)
	}
	if q.Count != 10 {
		t.Fatalf("count = %d", q.Count)
	}
}

func TestParseQueryAdvancedFeatures(t *testing.T) {
	q, err := search.ParseQueryValues("Patient", map[string][]string{
		"_include":   {"Patient:general-practitioner"},
		"name:exact": {"Smith"},
		"_sort":      {"-birthdate"},
	})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if len(q.Includes) != 1 || q.Includes[0].ParamCode != "general-practitioner" {
		t.Fatalf("includes = %#v", q.Includes)
	}
	if len(q.Params) != 1 || q.Params[0].Modifier != "exact" {
		t.Fatalf("params = %#v", q.Params)
	}
	if len(q.Sort) != 1 || q.Sort[0].Code != "birthdate" || q.Sort[0].Direction != search.SortDesc {
		t.Fatalf("sort = %#v", q.Sort)
	}
}

func TestParseQueryUnsupportedFeatures(t *testing.T) {
	_, err := search.ParseQueryValues("Patient", map[string][]string{"subject.name.family": {"Smith"}})
	if !errors.Is(err, search.ErrUnsupportedFeature) {
		t.Fatalf("expected chain depth error, got %v", err)
	}

	_, err = search.ParseQueryValues("Patient", map[string][]string{"name:missing": {"true"}})
	if !errors.Is(err, search.ErrUnsupportedFeature) {
		t.Fatalf("expected modifier error, got %v", err)
	}

	_, err = search.ParseQueryValues("Patient", map[string][]string{"_include": {"*:general-practitioner"}})
	if !errors.Is(err, search.ErrUnsupportedFeature) {
		t.Fatalf("expected wildcard include error, got %v", err)
	}
}

func TestResolveQueryUnknownParam(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)

	q, err := search.ParseQueryValues("Patient", map[string][]string{"unknown-param": {"x"}})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	_, err = search.ResolveQuery(reg, q)
	if !errors.Is(err, search.ErrUnknownParam) {
		t.Fatalf("expected ErrUnknownParam, got %v", err)
	}
}

func TestResolveQueryGenderSupported(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)

	q, err := search.ParseQueryValues("Patient", map[string][]string{"gender": {"female"}})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	resolved, err := search.ResolveQuery(reg, q)
	if err != nil {
		t.Fatalf("ResolveQuery: %v", err)
	}
	if len(resolved.Params) != 1 || resolved.Params[0].FieldKey != "token.gender" {
		t.Fatalf("resolved gender = %#v", resolved.Params)
	}
}

func TestBuildPlanMixedParams(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)
	parsed, err := search.ParseQueryValues("Patient", map[string][]string{
		"name": {"Doe"},
		"_id":  {"pat-1"},
	})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	resolved, err := search.ResolveQuery(reg, parsed)
	if err != nil {
		t.Fatalf("ResolveQuery: %v", err)
	}
	plan, err := search.BuildPlan(resolved)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.ParamPlans) != 2 {
		t.Fatalf("param plans = %d, want 2", len(plan.ParamPlans))
	}
	if plan.Sort[0].Code != "_id" {
		t.Fatalf("default sort = %#v", plan.Sort)
	}
}

func TestResolveQueryModifierValidation(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)

	q, err := search.ParseQueryValues("Patient", map[string][]string{"gender:contains": {"f"}})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	_, err = search.ResolveQuery(reg, q)
	if !errors.Is(err, search.ErrUnsupportedFeature) {
		t.Fatalf("expected unsupported modifier, got %v", err)
	}
}

func TestResolveQueryBirthdatePrefix(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)

	q, err := search.ParseQueryValues("Patient", map[string][]string{"birthdate": {"gt1990"}})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	resolved, err := search.ResolveQuery(reg, q)
	if err != nil {
		t.Fatalf("ResolveQuery: %v", err)
	}
	if len(resolved.Params) != 1 || resolved.Params[0].Values[0].Prefix != "gt" {
		t.Fatalf("birthdate prefix = %#v", resolved.Params[0].Values)
	}
}
