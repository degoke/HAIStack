package terminologyeval_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	terminologyeval "github.com/degoke/health-ai-stack/research/terminology-evaluation"
)

func TestGoldConceptMapMetrics(t *testing.T) {
	mapPath, casesPath := terminologyeval.TestdataPaths()
	metrics, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Failed > 0 {
		for _, r := range metrics.Results {
			if !r.Pass {
				t.Errorf("%s: got %s want %s target=%s", r.Code, r.GotClass, r.WantClass, r.Target)
			}
		}
	}
	assertObservedBuckets(t, metrics)
	if metrics.Provenance != 1 {
		t.Fatalf("provenance completeness = %v, want 1", metrics.Provenance)
	}
	if metrics.Accuracy != 1 || metrics.Precision != 1 || metrics.Recall != 1 {
		t.Fatalf("accuracy=%v precision=%v recall=%v, want 1", metrics.Accuracy, metrics.Precision, metrics.Recall)
	}
}

func TestMetricsGradeTranslatorNotGold(t *testing.T) {
	mapPath, _ := terminologyeval.TestdataPaths()
	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	payload := `{
  "sourceSystem": "http://haistack.dev/research/CodeSystem/toy-lab",
  "cases": [
    {"code": "HB", "class": "narrow", "target": "PANEL-WRONG"},
    {"code": "GLU", "class": "unmatched"}
  ]
}`
	if err := os.WriteFile(casesPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	metrics, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Exact != 1 || metrics.Narrow != 0 || metrics.Unmatched != 1 {
		t.Fatalf("observed buckets exact=%d narrow=%d unmatched=%d", metrics.Exact, metrics.Narrow, metrics.Unmatched)
	}
	if metrics.Accuracy != 0.5 {
		t.Fatalf("accuracy = %v, want 0.5", metrics.Accuracy)
	}
	if metrics.ByClass["narrow"].Recall != 0 || metrics.ByClass["narrow"].Support != 1 {
		t.Fatalf("narrow class score = %+v", metrics.ByClass["narrow"])
	}
	if metrics.ByClass["unmatched"].Precision != 1 || metrics.ByClass["unmatched"].Recall != 1 {
		t.Fatalf("unmatched class score = %+v", metrics.ByClass["unmatched"])
	}
	assertObservedBuckets(t, metrics)
}

func TestMacroPrecisionDiffersFromAccuracy(t *testing.T) {
	mapPath, _ := terminologyeval.TestdataPaths()
	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	// Three exact passes + WBC labeled exact but $translate returns broad.
	payload := `{
  "sourceSystem": "http://haistack.dev/research/CodeSystem/toy-lab",
  "cases": [
    {"code": "HB", "class": "exact", "target": "PANEL-HB"},
    {"code": "NA", "class": "exact", "target": "PANEL-NA"},
    {"code": "K", "class": "exact", "target": "PANEL-K"},
    {"code": "WBC", "class": "exact", "target": "PANEL-CBC"}
  ]
}`
	if err := os.WriteFile(casesPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	metrics, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Accuracy != 0.75 {
		t.Fatalf("accuracy = %v, want 0.75", metrics.Accuracy)
	}
	if metrics.Precision == metrics.Accuracy {
		t.Fatalf("macro precision %v must not equal accuracy %v", metrics.Precision, metrics.Accuracy)
	}
	if metrics.Precision != 0.5 {
		t.Fatalf("macro precision = %v, want 0.5 (exact P=1, broad P=0)", metrics.Precision)
	}
	if metrics.Recall != 0.75 {
		t.Fatalf("macro recall = %v, want 0.75 (exact R=0.75)", metrics.Recall)
	}
}

func TestGotClassFromTranslateEquivalence(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "conceptmap.json")
	casesPath := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(mapPath, []byte(`{
  "resourceType": "ConceptMap",
  "url": "http://haistack.dev/research/ConceptMap/lab-to-panel",
  "version": "1.0.0",
  "sourceUri": "http://haistack.dev/research/CodeSystem/toy-lab|1.0.0",
  "group": [{"source": "http://haistack.dev/research/CodeSystem/toy-lab", "target": "http://haistack.dev/research/CodeSystem/toy-panel",
    "element": [{"code": "HB", "target": [{"code": "PANEL-HB", "equivalence": "wider"}]}]
  }]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(casesPath, []byte(`{
  "sourceSystem": "http://haistack.dev/research/CodeSystem/toy-lab",
  "cases": [{"code": "HB", "class": "exact", "target": "PANEL-HB"}]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	metrics, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Broad != 1 || metrics.Exact != 0 {
		t.Fatalf("gotClass must come from $translate equivalence, got exact=%d broad=%d", metrics.Exact, metrics.Broad)
	}
	if metrics.Results[0].GotClass != "broad" || metrics.Results[0].Pass {
		t.Fatalf("result = %+v", metrics.Results[0])
	}
}

func TestProvenanceFromTranslateNotCases(t *testing.T) {
	mapPath, _ := terminologyeval.TestdataPaths()
	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(casesPath, []byte(`{
  "sourceSystem": "http://haistack.dev/research/CodeSystem/toy-lab",
  "cases": [{"code": "GLU", "class": "unmatched"}]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	metrics, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Provenance != 1 {
		t.Fatalf("provenance = %v, want 1 from Translate-resolved ConceptMap.sourceUri", metrics.Provenance)
	}
}

func TestProvenanceIncompleteWhenMapLacksVersion(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "conceptmap.json")
	casesPath := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(mapPath, []byte(`{
  "resourceType": "ConceptMap",
  "url": "http://haistack.dev/research/ConceptMap/lab-to-panel",
  "group": [{"element": [{"code": "GLU", "noMap": true}]}]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(casesPath, []byte(`{
  "sourceSystem": "http://haistack.dev/research/CodeSystem/toy-lab",
  "cases": [{"code": "GLU", "class": "unmatched"}]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	metrics, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Provenance != 0 {
		t.Fatalf("provenance = %v, want 0 when resolved ConceptMap lacks version and sourceUri", metrics.Provenance)
	}
}

func assertObservedBuckets(t *testing.T, metrics terminologyeval.Metrics) {
	t.Helper()
	var exact, narrow, broad, unmatched int
	for _, r := range metrics.Results {
		switch r.GotClass {
		case "exact":
			exact++
		case "narrow":
			narrow++
		case "broad":
			broad++
		default:
			unmatched++
		}
	}
	if metrics.Exact != exact || metrics.Narrow != narrow || metrics.Broad != broad || metrics.Unmatched != unmatched {
		t.Fatalf("class counts: metrics exact=%d narrow=%d broad=%d unmatched=%d results exact=%d narrow=%d broad=%d unmatched=%d",
			metrics.Exact, metrics.Narrow, metrics.Broad, metrics.Unmatched, exact, narrow, broad, unmatched)
	}
	if exact+narrow+broad+unmatched == 0 {
		t.Fatal("no scored cases")
	}
}
