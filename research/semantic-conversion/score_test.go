package semanticconversion_test

import (
	"testing"

	semanticconversion "github.com/degoke/health-ai-stack/research/semantic-conversion"
)

func TestCorpusScore(t *testing.T) {
	pairs := semanticconversion.Corpus()
	if len(pairs) < 50 {
		t.Fatalf("corpus size = %d, want >= 50", len(pairs))
	}
	types := map[string]int{}
	for _, p := range pairs {
		types[p.ResourceType]++
	}
	for _, rt := range []string{"Patient", "Observation", "Condition", "MedicationRequest"} {
		if types[rt] == 0 {
			t.Fatalf("missing resource type %s", rt)
		}
	}
	report, err := semanticconversion.ScoreCorpus(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed > 0 {
		for _, s := range report.Scores {
			if !s.StructuralOK || !s.SemanticOK {
				t.Errorf("%s: structural=%v semantic=%v errors=%v", s.ID, s.StructuralOK, s.SemanticOK, s.Errors)
			}
		}
	}
	if report.Pairs != len(pairs) {
		t.Fatalf("scored %d of %d", report.Pairs, len(pairs))
	}
}
