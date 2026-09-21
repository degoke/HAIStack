package main

import "testing"

func TestCorpusSize(t *testing.T) {
	pairs := Corpus()
	if len(pairs) < 50 {
		t.Fatalf("corpus size = %d, want ≥50", len(pairs))
	}
	seen := map[string]struct{}{}
	types := map[string]int{}
	for _, p := range pairs {
		if p.ID == "" || p.ResourceType == "" {
			t.Fatalf("incomplete pair: %+v", p)
		}
		if _, dup := seen[p.ID]; dup {
			t.Fatalf("duplicate pair id %s", p.ID)
		}
		seen[p.ID] = struct{}{}
		types[p.ResourceType]++
		if len(p.R4) == 0 || len(p.R5) == 0 {
			t.Fatalf("%s missing JSON", p.ID)
		}
	}
	for _, rt := range []string{"Patient", "Observation", "Condition", "MedicationRequest"} {
		if types[rt] == 0 {
			t.Fatalf("missing resource type %s", rt)
		}
	}
}

func TestScoreAll(t *testing.T) {
	report, err := ScoreAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed > 0 {
		t.Fatalf("failures: %+v", report.Failures)
	}
	if report.Passed != report.Pairs {
		t.Fatalf("passed %d of %d", report.Passed, report.Pairs)
	}
	if report.LossFlags == 0 {
		t.Fatal("expected information-loss flags")
	}
}
