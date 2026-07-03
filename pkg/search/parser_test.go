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
	if len(nameClauses[0].Values) != 2 || nameClauses[0].Values[0] != "Smith" || nameClauses[0].Values[1] != "Jones" {
		t.Fatalf("first name clause = %#v", nameClauses[0].Values)
	}
	if len(nameClauses[1].Values) != 1 || nameClauses[1].Values[0] != "Brown" {
		t.Fatalf("second name clause = %#v", nameClauses[1].Values)
	}
	if q.Count != 10 {
		t.Fatalf("count = %d", q.Count)
	}
}

func TestParseQueryUnsupportedFeatures(t *testing.T) {
	_, err := search.ParseQueryValues("Patient", map[string][]string{"_include": {"Patient:general-practitioner"}})
	if !errors.Is(err, search.ErrUnsupportedFeature) {
		t.Fatalf("expected ErrUnsupportedFeature, got %v", err)
	}

	_, err = search.ParseQueryValues("Patient", map[string][]string{"general-practitioner.name": {"Smith"}})
	if !errors.Is(err, search.ErrUnsupportedFeature) {
		t.Fatalf("expected chained search error, got %v", err)
	}

	_, err = search.ParseQueryValues("Patient", map[string][]string{"name:exact": {"Smith"}})
	if !errors.Is(err, search.ErrUnsupportedFeature) {
		t.Fatalf("expected modifier error, got %v", err)
	}
}

func TestResolveQueryUnknownAndUnsupported(t *testing.T) {
	snapshot := testSnapshot(t, "Patient")
	reg := search.NewSnapshotRegistry(snapshot)

	q, err := search.ParseQueryValues("Patient", map[string][]string{"gender": {"female"}})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	_, err = search.ResolveQuery(reg, q)
	if !errors.Is(err, search.ErrUnsupportedParam) {
		t.Fatalf("expected ErrUnsupportedParam, got %v", err)
	}

	q, err = search.ParseQueryValues("Patient", map[string][]string{"unknown-param": {"x"}})
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	_, err = search.ResolveQuery(reg, q)
	if !errors.Is(err, search.ErrUnknownParam) {
		t.Fatalf("expected ErrUnknownParam, got %v", err)
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
