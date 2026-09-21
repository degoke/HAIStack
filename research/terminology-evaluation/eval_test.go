package terminologyeval_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
		t.Fatalf("provenance completeness = %v, want 1 from ConceptMap body (store key is %s)", metrics.Provenance, terminologyeval.EvalStoreCanonical)
	}
	if metrics.Accuracy != 1 {
		t.Fatalf("accuracy=%v, want 1 (gold consistency, not a quality headline)", metrics.Accuracy)
	}
}

func TestHeldOutMapGradedAgainstAuthoredCases(t *testing.T) {
	_, casesPath := terminologyeval.TestdataPaths()
	metrics, err := terminologyeval.Evaluate(context.Background(), terminologyeval.HeldOutMapPath(), casesPath, terminologyeval.FixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Accuracy != 0.875 {
		t.Fatalf("held-out accuracy = %v, want 0.875 (WBC exact vs authored broad)", metrics.Accuracy)
	}
	exact := metrics.ByClass["exact"]
	if ptrVal(exact.Precision) != 0.75 || ptrVal(exact.Recall) != 1 || exact.Support != 3 || exact.Predicted != 4 {
		t.Fatalf("exact byClass = %+v, want P=0.75 R=1 support=3 predicted=4", exact)
	}
	broad := metrics.ByClass["broad"]
	if broad.Precision != nil {
		t.Fatalf("broad precision must be omitted when predicted=0, got %v", *broad.Precision)
	}
	if ptrVal(broad.Recall) != 0 || broad.Support != 1 || broad.Predicted != 0 {
		t.Fatalf("broad byClass = %+v, want R=0 support=1 predicted=0", broad)
	}
	if ptrVal(exact.Precision) == metrics.Accuracy {
		t.Fatalf("class precision %v must not equal accuracy %v", ptrVal(exact.Precision), metrics.Accuracy)
	}
	var wbc terminologyeval.CaseResult
	for _, r := range metrics.Results {
		if r.Code == "WBC" {
			wbc = r
		}
	}
	if wbc.GotClass != "exact" || wbc.WantClass != "broad" || wbc.Pass {
		t.Fatalf("WBC held-out result = %+v", wbc)
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
	if ptrVal(metrics.ByClass["narrow"].Recall) != 0 || metrics.ByClass["narrow"].Support != 1 {
		t.Fatalf("narrow class score = %+v", metrics.ByClass["narrow"])
	}
	if ptrVal(metrics.ByClass["unmatched"].Precision) != 1 || ptrVal(metrics.ByClass["unmatched"].Recall) != 1 {
		t.Fatalf("unmatched class score = %+v", metrics.ByClass["unmatched"])
	}
	assertObservedBuckets(t, metrics)
}

func TestWrongTargetDoesNotCountClassFalsePositive(t *testing.T) {
	mapPath, _ := terminologyeval.TestdataPaths()
	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	payload := `{
  "sourceSystem": "http://haistack.dev/research/CodeSystem/toy-lab",
  "cases": [
    {"code": "HB", "class": "exact", "target": "PANEL-WRONG"}
  ]
}`
	if err := os.WriteFile(casesPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	metrics, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Accuracy != 0 || metrics.Failed != 1 {
		t.Fatalf("wrong target must fail accuracy, got accuracy=%v failed=%d", metrics.Accuracy, metrics.Failed)
	}
	exact := metrics.ByClass["exact"]
	if ptrVal(exact.Precision) != 1 || ptrVal(exact.Recall) != 1 || exact.Predicted != 1 {
		t.Fatalf("right class with wrong target must not be a class FP: %+v", exact)
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

func TestMissingMapURLStillTranslatesButProvenanceZero(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "conceptmap.json")
	casesPath := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(mapPath, []byte(`{"resourceType":"ConceptMap","group":[{"element":[{"code":"GLU","noMap":true}]}]}`), 0o600); err != nil {
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
		t.Fatalf("provenance = %v, want 0 when ConceptMap body has no url", metrics.Provenance)
	}
	if metrics.Results[0].GotClass != "unmatched" {
		t.Fatalf("result = %+v", metrics.Results[0])
	}
}

func TestEvaluateSourceDoesNotWriteAudit(t *testing.T) {
	raw, err := os.ReadFile(terminologyeval.SourceFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "audit.LogTerminologyTranslate") {
		t.Fatal("Evaluate must not write terminology.translate events; provenance is emitted inside pkg/terminology.Translate")
	}
	if !strings.Contains(string(raw), terminologyeval.EvalStoreCanonical) {
		t.Fatal("Evaluate must look up the map by EvalStoreCanonical, not ConceptMap.url")
	}
}

func TestGoldFileOmitsMapIdentity(t *testing.T) {
	_, casesPath := terminologyeval.TestdataPaths()
	raw, err := os.ReadFile(casesPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"conceptMapUrl", "conceptMapVersion", "sourceSystemVersion"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("cases.json must not publish %s", key)
		}
	}
}

func ptrVal(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
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
