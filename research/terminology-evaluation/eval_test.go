package terminologyeval_test

import (
	"context"
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
	if metrics.Exact == 0 || metrics.Narrow == 0 || metrics.Broad == 0 || metrics.Unmatched == 0 {
		t.Fatalf("expected all equivalence buckets, got exact=%d narrow=%d broad=%d unmatched=%d", metrics.Exact, metrics.Narrow, metrics.Broad, metrics.Unmatched)
	}
	if metrics.Provenance != 1 {
		t.Fatalf("provenance completeness = %v, want 1", metrics.Provenance)
	}
	if metrics.Precision != 1 {
		t.Fatalf("precision = %v, want 1", metrics.Precision)
	}
}
