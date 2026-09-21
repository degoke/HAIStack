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

// Authored gold URLs/displays are literals here so testdata is not tied to
// convert.go's emission constants. Drift fails TestCorpusGoldIsAuthoredOracle.
const (
	goldInformantSystem  = "http://terminology.hl7.org/CodeSystem/provenance-participant-type"
	goldInformantDisplay = "Informant"
	goldInterpSystem     = "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation"
	goldInterpDisplay    = "Normal"
	goldToyMedication    = "http://haistack.dev/research/CodeSystem/toy-med"
	goldPatientDiff      = "http://hl7.org/fhir/R5/patient.html#diff"
	goldConditionDiff    = "http://hl7.org/fhir/R5/condition.html#diff"
	goldObservationDiff  = "http://hl7.org/fhir/R5/observation.html#diff"
	goldMedicationDiff   = "http://hl7.org/fhir/R5/medicationrequest.html#diff"
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
		r4obj := jsonObj(t, p.R4)
		r5obj := jsonObj(t, p.R5)
		assertCopyThrough(t, p.ID, r4obj, r5obj, "resourceType", "id")
		switch p.Category {
		case semanticconversion.CategoryUnchanged:
			if !bytesEqualCanonical(p.R4, p.R5) {
				t.Errorf("%s: unchanged gold R4 must equal gold R5 in testdata", p.ID)
			}
			unchanged++
		case semanticconversion.CategoryRemoved:
			assertCopyThrough(t, p.ID, r4obj, r5obj, "gender")
			if !bytes.Contains(p.R4, []byte(`"animal"`)) || bytes.Contains(p.R5, []byte(`"animal"`)) {
				t.Errorf("%s: removed gold must drop Patient.animal on R5 only", p.ID)
			}
			if p.Spec != goldPatientDiff {
				t.Errorf("%s: spec = %q, want Patient R5 diff", p.ID, p.Spec)
			}
			transformed++
		case semanticconversion.CategoryRenamed:
			assertCopyThrough(t, p.ID, r4obj, r5obj, "clinicalStatus", "code", "subject")
			assertActorFromAsserter(t, p.ID, r4obj, r5obj)
			if p.Spec != goldConditionDiff {
				t.Errorf("%s: spec = %q, want Condition R5 diff", p.ID, p.Spec)
			}
			if bytes.Contains(p.R4, []byte("participant")) || !bytes.Contains(p.R4, []byte("asserter")) {
				t.Errorf("%s: R4 gold should keep asserter", p.ID)
			}
			if bytes.Contains(p.R5, []byte("asserter")) || !bytes.Contains(p.R5, []byte("participant")) {
				t.Errorf("%s: R5 gold should keep participant", p.ID)
			}
			if strings.Contains(r4, goldInformantDisplay) {
				t.Errorf("%s: R4 must not contain the authored Informant display", p.ID)
			}
			if !strings.Contains(r5, goldInformantSystem) || !strings.Contains(r5, goldInformantDisplay) {
				t.Errorf("%s: authored gold R5 must include informant system and display", p.ID)
			}
			transformed++
		case semanticconversion.CategoryCardinality:
			assertCopyThrough(t, p.ID, r4obj, r5obj, "status", "code")
			if p.Spec != goldObservationDiff {
				t.Errorf("%s: spec = %q, want Observation R5 diff", p.ID, p.Spec)
			}
			if strings.Contains(r4, goldInterpSystem) {
				t.Errorf("%s: R4 must not already contain the authored interpretation system", p.ID)
			}
			if !strings.Contains(r5, goldInterpSystem) || !strings.Contains(r5, goldInterpDisplay) {
				t.Errorf("%s: authored gold R5 must stamp interpretation system and Normal display", p.ID)
			}
			transformed++
		case semanticconversion.CategoryCodeableConcept, semanticconversion.CategoryTypeChange:
			assertCopyThrough(t, p.ID, r4obj, r5obj, "status", "intent", "subject")
			assertReasonFromR4(t, p.ID, r4obj, r5obj)
			if p.Spec != goldMedicationDiff {
				t.Errorf("%s: spec = %q, want MedicationRequest R5 diff", p.ID, p.Spec)
			}
			if bytes.Contains(p.R5, []byte("medicationCodeableConcept")) || !bytes.Contains(p.R5, []byte(`"medication"`)) {
				t.Errorf("%s: R5 gold should use medication CodeableReference", p.ID)
			}
			if r4HasCodedMedication(r4obj) && !goldHasToyMed(r5obj) {
				t.Errorf("%s: authored gold R5 must stamp toy-med system on coded medication", p.ID)
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
		if p.Spec != "" {
			if strings.Contains(string(p.R4), `"source":"`+p.Spec) || strings.Contains(string(p.R4), `"source": "`+p.Spec) {
				t.Errorf("%s: R4 must not contain authored meta.source", p.ID)
			}
			if !goldHasMetaSource(p.R5, p.Spec) {
				t.Errorf("%s: gold R5 must author meta.source = spec (not produced by Convert)", p.ID)
			}
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
	report, err := semanticconversion.ScoreCorpus(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed > 0 {
		t.Fatalf("converter must satisfy authored constraints: %+v", report.Scores)
	}
	var distinguished int
	for _, pair := range pairs {
		if pair.Spec == "" {
			continue
		}
		got, _, err := semanticconversion.ConvertR4ToR5(pair.ResourceType, pair.R4)
		if err != nil {
			t.Errorf("%s: convert: %v", pair.ID, err)
			continue
		}
		if bytesEqualCanonical(got, pair.R5) {
			t.Errorf("%s: gold R5 must not equal Convert output (authored meta.source)", pair.ID)
		}
		if goldHasMetaSource(got, pair.Spec) {
			t.Errorf("%s: Convert must not emit authored meta.source", pair.ID)
		}
		distinguished++
	}
	if distinguished == 0 {
		t.Fatal("expected transformed pairs with spec")
	}
}

func TestInformationLossUsesDetectedFlags(t *testing.T) {
	pairs, err := semanticconversion.LoadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	var pair semanticconversion.Pair
	for _, p := range pairs {
		if p.ResourceType == "Patient" && p.Category == semanticconversion.CategoryUnchanged &&
			!bytes.Contains(p.R4, []byte("animal")) {
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

func goldHasMetaSource(raw []byte, spec string) bool {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	meta, _ := obj["meta"].(map[string]any)
	src, _ := meta["source"].(string)
	return src == spec
}

func jsonObj(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	return obj
}

func assertCopyThrough(t *testing.T, id string, r4, r5 map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := r4[key]; !ok {
			t.Errorf("%s: R4 missing copy-through field %s", id, key)
			continue
		}
		if !jsonValueEqual(r4[key], r5[key]) {
			t.Errorf("%s: gold R5 %s must copy through from R4 (testdata authorship, no Convert)", id, key)
		}
	}
}

func jsonValueEqual(a, b any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}

func assertActorFromAsserter(t *testing.T, id string, r4, r5 map[string]any) {
	t.Helper()
	parts, _ := r5["participant"].([]any)
	if len(parts) == 0 {
		t.Errorf("%s: gold R5 missing participant", id)
		return
	}
	pm, _ := parts[0].(map[string]any)
	if !jsonValueEqual(r4["asserter"], pm["actor"]) {
		t.Errorf("%s: gold participant.actor must equal R4 asserter (authorship, no Convert)", id)
	}
}

func assertReasonFromR4(t *testing.T, id string, r4, r5 map[string]any) {
	t.Helper()
	var want []any
	if rc, ok := r4["reasonCode"].([]any); ok {
		for _, item := range rc {
			want = append(want, map[string]any{"concept": item})
		}
	}
	if rr, ok := r4["reasonReference"].([]any); ok {
		for _, item := range rr {
			want = append(want, map[string]any{"reference": item})
		}
	}
	if len(want) == 0 {
		return
	}
	if !jsonValueEqual(want, r5["reason"]) {
		t.Errorf("%s: gold reason must wrap R4 reasonCode/reasonReference (authorship, no Convert)", id)
	}
}

func r4HasCodedMedication(r4 map[string]any) bool {
	med, _ := r4["medicationCodeableConcept"].(map[string]any)
	coding, _ := med["coding"].([]any)
	for _, item := range coding {
		cm, _ := item.(map[string]any)
		if _, ok := cm["code"]; ok {
			return true
		}
	}
	return false
}

func goldHasToyMed(r5 map[string]any) bool {
	meds, _ := r5["medication"].([]any)
	if len(meds) == 0 {
		return false
	}
	m, _ := meds[0].(map[string]any)
	concept, _ := m["concept"].(map[string]any)
	coding, _ := concept["coding"].([]any)
	for _, item := range coding {
		cm, _ := item.(map[string]any)
		sys, _ := cm["system"].(string)
		if sys == goldToyMedication {
			return true
		}
	}
	return false
}
