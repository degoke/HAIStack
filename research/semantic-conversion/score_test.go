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
	pairs, err := semanticconversion.ParseCorpus(raw)
	if err != nil {
		t.Fatal(err)
	}
	var unchanged, transformed int
	for _, p := range pairs {
		r4, r5 := string(p.R4), string(p.R5)
		switch p.Category {
		case semanticconversion.CategoryUnchanged:
			if len(p.InformationLoss) == 0 {
				if !bytesEqualCanonical(p.R4, p.R5) {
					t.Errorf("%s: unchanged gold R4 must equal gold R5 in testdata", p.ID)
				}
				unchanged++
				continue
			}
			if bytes.Contains(p.R4, []byte(`"animal"`)) && bytes.Contains(p.R5, []byte(`"animal"`)) {
				t.Errorf("%s: authored gold R5 must omit Patient.animal", p.ID)
			}
			if p.Spec == "" {
				t.Errorf("%s: animal-loss gold must cite the Patient R5 diff", p.ID)
			}
			transformed++
		case semanticconversion.CategoryRenamed:
			if p.Spec == "" {
				t.Errorf("%s: renamed gold must cite a spec URL", p.ID)
			}
			if bytes.Contains(p.R4, []byte("participant")) || !bytes.Contains(p.R4, []byte("asserter")) {
				t.Errorf("%s: R4 gold should keep asserter", p.ID)
			}
			if bytes.Contains(p.R5, []byte("asserter")) || !bytes.Contains(p.R5, []byte("participant")) {
				t.Errorf("%s: R5 gold should keep participant", p.ID)
			}
			if strings.Contains(r4, semanticconversion.ParticipantInformantDisplay) {
				t.Errorf("%s: R4 must not contain the authored Informant display", p.ID)
			}
			if !strings.Contains(r5, semanticconversion.ParticipantInformantSystem) ||
				!strings.Contains(r5, semanticconversion.ParticipantInformantDisplay) {
				t.Errorf("%s: authored gold R5 must include informant system and display", p.ID)
			}
			transformed++
		case semanticconversion.CategoryCardinality:
			if p.Spec == "" {
				t.Errorf("%s: cardinality gold must cite a spec URL", p.ID)
			}
			if strings.Contains(r4, semanticconversion.ObservationInterpretationSystem) {
				t.Errorf("%s: R4 must not already contain the authored interpretation system", p.ID)
			}
			if !strings.Contains(r5, semanticconversion.ObservationInterpretationSystem) ||
				!strings.Contains(r5, semanticconversion.ObservationInterpretationNDisplay) {
				t.Errorf("%s: authored gold R5 must stamp interpretation system and Normal display", p.ID)
			}
			transformed++
		case semanticconversion.CategoryCodeableConcept, semanticconversion.CategoryTypeChange:
			if p.Spec == "" {
				t.Errorf("%s: medication gold must cite a spec URL", p.ID)
			}
			if bytes.Contains(p.R5, []byte("medicationCodeableConcept")) || !bytes.Contains(p.R5, []byte(`"medication"`)) {
				t.Errorf("%s: R5 gold should use medication CodeableReference", p.ID)
			}
			if bytes.Contains(p.R4, []byte("medicationCodeableConcept")) && strings.Contains(r4, `"code"`) &&
				!strings.Contains(r4, semanticconversion.MedicationRxNormSystem) &&
				!strings.Contains(r5, semanticconversion.MedicationRxNormSystem) {
				t.Errorf("%s: authored gold R5 must stamp RxNorm system on coded medication", p.ID)
			}
			if p.Category == semanticconversion.CategoryTypeChange {
				if bytes.Contains(p.R4, []byte(`"reported"`)) || !bytes.Contains(p.R5, []byte(`"reported"`)) {
					t.Errorf("%s: type-change gold must author reported on R5 only", p.ID)
				}
			}
			transformed++
		default:
			t.Errorf("%s: unknown category %s", p.ID, p.Category)
		}
	}
	if unchanged == 0 || transformed < 20 {
		t.Fatalf("authorship coverage unchanged=%d transformed=%d", unchanged, transformed)
	}
}

func TestConverterImplementsAuthoredGold(t *testing.T) {
	pairs, err := semanticconversion.LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range pairs {
		got, _, err := semanticconversion.ConvertR4ToR5(pair.ResourceType, pair.R4)
		if err != nil {
			t.Errorf("%s: convert: %v", pair.ID, err)
			continue
		}
		if !bytesEqualCanonical(got, pair.R5) {
			t.Errorf("%s: converter must implement authored gold\ngot  %s\ngold %s", pair.ID, got, pair.R5)
		}
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
