package semanticconversion_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	var usedFHIRPath, usedJSONPath bool
	for _, s := range report.Scores {
		switch s.R4Engine {
		case "fhirpath":
			usedFHIRPath = true
		case "json-path":
			usedJSONPath = true
		}
		if s.R5Engine != "json-path" {
			t.Errorf("%s: r5Engine=%q, want json-path", s.ID, s.R5Engine)
		}
	}
	if !usedFHIRPath {
		t.Fatal("expected pkg/fhirpath to score loadable R4 instances")
	}
	if !usedJSONPath {
		t.Fatal("expected JSON-path fallback for R4 instances the protobuf codec cannot load")
	}
}

func TestCorpusGoldIsIndependentTestdata(t *testing.T) {
	pairs := semanticconversion.Corpus()
	parsed, err := semanticconversion.ParseCorpus(mustReadCorpus(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != len(pairs) {
		t.Fatalf("testdata pairs = %d, Corpus() = %d", len(parsed), len(pairs))
	}
	got, _, err := semanticconversion.ConvertR4ToR5(pairs[0].ResourceType, pairs[0].R4)
	if err != nil {
		t.Fatal(err)
	}
	if bytesEqualCanonical(got, pairs[0].R4) && pairs[0].Category != semanticconversion.CategoryUnchanged {
		t.Fatal("gold R5 must be stored independently; converter is scored against testdata, not identity")
	}
}

func mustReadCorpus(t *testing.T) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "testdata", "corpus.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func bytesEqualCanonical(a, b []byte) bool {
	var left, right any
	if err := json.Unmarshal(a, &left); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &right); err != nil {
		return false
	}
	lb, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rb, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return bytes.Equal(lb, rb)
}
