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
	if metrics.Precision != 1 {
		t.Fatalf("precision = %v, want 1", metrics.Precision)
	}
	if metrics.Recall != 1 {
		t.Fatalf("recall = %v, want 1", metrics.Recall)
	}
}

func TestMetricsGradeTranslatorNotGold(t *testing.T) {
	mapPath, _ := terminologyeval.TestdataPaths()
	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	// Gold labels HB as narrow with a wrong target so the translator is scored,
	// not the gold file. The ConceptMap still produces gotClass=exact.
	payload := `{
  "conceptMapUrl": "http://haistack.dev/research/ConceptMap/lab-to-panel",
  "conceptMapVersion": "1.0.0",
  "sourceSystem": "http://haistack.dev/research/CodeSystem/toy-lab",
  "sourceSystemVersion": "1.0.0",
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
		t.Fatalf("observed buckets exact=%d narrow=%d unmatched=%d (must follow gotClass)", metrics.Exact, metrics.Narrow, metrics.Unmatched)
	}
	if metrics.Failed != 1 || metrics.Passed != 1 {
		t.Fatalf("pass/fail = %d/%d, want 1/1", metrics.Passed, metrics.Failed)
	}
	if metrics.Precision != 0 {
		t.Fatalf("precision = %v, want 0 (one false-positive match)", metrics.Precision)
	}
	if metrics.Recall != 0 {
		t.Fatalf("recall = %v, want 0 (gold match was wrong)", metrics.Recall)
	}
	assertObservedBuckets(t, metrics)
}

func TestProvenanceReadsAuditEvent(t *testing.T) {
	mapPath, _ := terminologyeval.TestdataPaths()
	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	payload := `{
  "conceptMapUrl": "http://haistack.dev/research/ConceptMap/lab-to-panel",
  "conceptMapVersion": "1.0.0",
  "sourceSystem": "http://haistack.dev/research/CodeSystem/toy-lab",
  "cases": [
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
	if metrics.Provenance != 0 {
		t.Fatalf("provenance completeness = %v, want 0 when audit event lacks sourceSystemVersion", metrics.Provenance)
	}
	if len(metrics.Results) != 1 || metrics.Results[0].Provenance {
		t.Fatal("expected provenanceComplete=false on the case result")
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
		t.Fatalf("class counts follow gold not translator: metrics exact=%d narrow=%d broad=%d unmatched=%d results exact=%d narrow=%d broad=%d unmatched=%d",
			metrics.Exact, metrics.Narrow, metrics.Broad, metrics.Unmatched, exact, narrow, broad, unmatched)
	}
	if exact+narrow+broad+unmatched == 0 {
		t.Fatal("no scored cases")
	}
}
