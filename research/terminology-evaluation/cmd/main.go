// Command research-terminology-evaluation scores the gold ConceptMap
// (consistency) and a mismatched-label fixture (class metrics).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	terminologyeval "github.com/degoke/health-ai-stack/research/terminology-evaluation"
)

func main() {
	mapPath, casesPath := terminologyeval.TestdataPaths()
	gold, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-terminology-evaluation: %v\n", err)
		os.Exit(1)
	}
	classMetrics, err := terminologyeval.Evaluate(context.Background(), mapPath, terminologyeval.MismatchCasesPath(), terminologyeval.FixedNow())
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-terminology-evaluation: class metrics: %v\n", err)
		os.Exit(1)
	}
	report := map[string]any{
		"goldConsistency": map[string]any{
			"exact":                  gold.Exact,
			"narrow":                 gold.Narrow,
			"broad":                  gold.Broad,
			"unmatched":              gold.Unmatched,
			"passed":                 gold.Passed,
			"failed":                 gold.Failed,
			"accuracy":               gold.Accuracy,
			"provenanceCompleteness": gold.Provenance,
			"results":                gold.Results,
			"note":                   "accuracy 1.0 means $translate implements this map, not an independent quality signal",
		},
		"classMetrics": map[string]any{
			"accuracy": classMetrics.Accuracy,
			"passed":   classMetrics.Passed,
			"failed":   classMetrics.Failed,
			"byClass":  classMetrics.ByClass,
			"note":     "mismatched labels against the same map; byClass is not the pass rate",
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "research-terminology-evaluation: encode: %v\n", err)
		os.Exit(1)
	}
	if gold.Failed > 0 {
		os.Exit(1)
	}
}
