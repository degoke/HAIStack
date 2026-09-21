package main

import (
	"bytes"
	"testing"
)

var mappingCategories = []string{"renamed", "type", "cardinality", "information_loss"}

func TestCorpusSizeAndDensity(t *testing.T) {
	pairs := Corpus()
	if len(pairs) < 50 {
		t.Fatalf("corpus size = %d, want ≥50", len(pairs))
	}
	seen := map[string]struct{}{}
	types := map[string]int{}
	cats := map[string]int{}
	differing := 0
	for _, p := range pairs {
		if p.ID == "" || p.ResourceType == "" {
			t.Fatalf("incomplete pair: %+v", p)
		}
		if _, dup := seen[p.ID]; dup {
			t.Fatalf("duplicate pair id %s", p.ID)
		}
		seen[p.ID] = struct{}{}
		types[p.ResourceType]++
		cats[p.Category]++
		if len(p.R4) == 0 || len(p.R5) == 0 {
			t.Fatalf("%s missing JSON", p.ID)
		}
		if !bytes.Equal(p.R4, p.R5) {
			differing++
		}
		mustDiffer := false
		for _, c := range mappingCategories {
			if p.Category == c {
				mustDiffer = true
				break
			}
		}
		if mustDiffer && bytes.Equal(p.R4, p.R5) {
			t.Fatalf("%s category %s is an identical R4/R5 copy", p.ID, p.Category)
		}
		if p.Category == "identity" && !bytes.Equal(p.R4, p.R5) {
			t.Fatalf("%s identity pair must keep identical R4/R5 JSON", p.ID)
		}
		if len(p.R4FHIRPath) == 0 {
			t.Fatalf("%s missing R4 FHIRPath assertions", p.ID)
		}
		if len(p.R5JSON) == 0 {
			t.Fatalf("%s missing R5 JSON checks", p.ID)
		}
	}
	for _, rt := range []string{"Patient", "Observation", "Condition", "MedicationRequest"} {
		if types[rt] == 0 {
			t.Fatalf("missing resource type %s", rt)
		}
	}
	for _, c := range append(append([]string{}, mappingCategories...), "identity", "codeableconcept") {
		if cats[c] == 0 {
			t.Fatalf("missing category %s", c)
		}
	}
	if differing < 30 {
		t.Fatalf("differing R4/R5 pairs = %d, want ≥30", differing)
	}
	if cats["identity"]*2 > len(pairs) {
		t.Fatalf("identity pairs dominate the corpus (%d/%d)", cats["identity"], len(pairs))
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
	if report.Differing < 30 {
		t.Fatalf("differing pairs = %d, want ≥30", report.Differing)
	}
}
