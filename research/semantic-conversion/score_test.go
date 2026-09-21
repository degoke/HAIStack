package semanticconversion_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	semanticconversion "github.com/degoke/health-ai-stack/research/semantic-conversion"
)

func TestCorpusScore(t *testing.T) {
	pairs, err := semanticconversion.LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
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

func TestCorpusGoldIsAuthoredOracle(t *testing.T) {
	raw := mustReadCorpus(t)
	if !bytes.Contains(raw, []byte(semanticconversion.ParticipantInformantSystem)) {
		t.Fatal("gold testdata must author the R5 informant function system; converter follows gold")
	}
	pairs, err := semanticconversion.ParseCorpus(raw)
	if err != nil {
		t.Fatal(err)
	}
	var pair semanticconversion.Pair
	for _, p := range pairs {
		if p.Category == semanticconversion.CategoryRenamed && p.ResourceType == "Condition" {
			pair = p
			break
		}
	}
	if pair.ID == "" {
		t.Fatal("missing renamed Condition pair")
	}
	if bytes.Contains(pair.R4, []byte("participant")) || !bytes.Contains(pair.R4, []byte("asserter")) {
		t.Fatalf("R4 gold should keep asserter, got %s", pair.R4)
	}
	if bytes.Contains(pair.R5, []byte("asserter")) || !bytes.Contains(pair.R5, []byte("participant")) {
		t.Fatalf("R5 gold should keep participant, got %s", pair.R5)
	}
	if !bytes.Contains(pair.R5, []byte(semanticconversion.ParticipantInformantSystem)) {
		t.Fatal("authored gold R5 must require the HL7 informant system")
	}
	got, _, err := semanticconversion.ConvertR4ToR5(pair.ResourceType, pair.R4)
	if err != nil {
		t.Fatal(err)
	}
	if !bytesEqualCanonical(got, pair.R5) {
		t.Fatalf("converter must implement authored gold, not the reverse\ngot  %s\ngold %s", got, pair.R5)
	}
}

func TestInformationLossUsesDetectedFlags(t *testing.T) {
	pairs, err := semanticconversion.LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	var pair semanticconversion.Pair
	for _, p := range pairs {
		if p.ResourceType == "Patient" && len(p.InformationLoss) == 0 && !bytes.Contains(p.R4, []byte("animal")) {
			pair = p
			break
		}
	}
	if pair.ID == "" {
		t.Fatal("missing Patient pair without animal")
	}
	pair.InformationLoss = []string{"Patient.animal removed in R5"}
	report, err := semanticconversion.ScoreCorpus([]semanticconversion.Pair{pair})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || report.Scores[0].SemanticOK {
		t.Fatalf("declared loss must fail when ConvertR4ToR5 does not detect it: %+v", report.Scores)
	}
	if !containsError(report.Scores[0].Errors, "missing declared information-loss flags") {
		t.Fatalf("errors = %v", report.Scores[0].Errors)
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

func containsError(errs []string, want string) bool {
	for _, err := range errs {
		if strings.Contains(err, want) {
			return true
		}
	}
	return false
}
