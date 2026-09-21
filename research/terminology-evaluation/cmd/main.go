// Command research-terminology-evaluation scores gold map consistency and
// a held-out ConceptMap against the same authored cases.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	terminologyeval "github.com/degoke/health-ai-stack/research/terminology-evaluation"
)

type goldBlock struct {
	Exact                  int                          `json:"exact"`
	Narrow                 int                          `json:"narrow"`
	Broad                  int                          `json:"broad"`
	Unmatched              int                          `json:"unmatched"`
	Passed                 int                          `json:"passed"`
	Failed                 int                          `json:"failed"`
	Accuracy               float64                      `json:"accuracy"`
	ProvenanceCompleteness float64                      `json:"provenanceCompleteness"`
	Results                []terminologyeval.CaseResult `json:"results"`
	Note                   string                       `json:"note"`
}

type classBlock struct {
	Accuracy float64                               `json:"accuracy"`
	Passed   int                                   `json:"passed"`
	Failed   int                                   `json:"failed"`
	ByClass  map[string]terminologyeval.ClassScore `json:"byClass"`
	Note     string                                `json:"note"`
}

type publishedReport struct {
	GoldConsistency goldBlock  `json:"goldConsistency"`
	ClassMetrics    classBlock `json:"classMetrics"`
}

func main() {
	mapPath, casesPath := terminologyeval.TestdataPaths()
	gold, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-terminology-evaluation: %v\n", err)
		os.Exit(1)
	}
	held, err := terminologyeval.Evaluate(context.Background(), terminologyeval.HeldOutMapPath(), casesPath, terminologyeval.FixedNow())
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-terminology-evaluation: held-out: %v\n", err)
		os.Exit(1)
	}
	report := publishedReport{
		GoldConsistency: goldBlock{
			Exact:                  gold.Exact,
			Narrow:                 gold.Narrow,
			Broad:                  gold.Broad,
			Unmatched:              gold.Unmatched,
			Passed:                 gold.Passed,
			Failed:                 gold.Failed,
			Accuracy:               gold.Accuracy,
			ProvenanceCompleteness: gold.Provenance,
			Results:                gold.Results,
			Note:                   "accuracy 1.0 means $translate implements conceptmap.json, not an independent quality signal",
		},
		ClassMetrics: classBlock{
			Accuracy: held.Accuracy,
			Passed:   held.Passed,
			Failed:   held.Failed,
			ByClass:  held.ByClass,
			Note:     "held-out ConceptMap (WBC equivalent vs authored wider) scored against cases.json",
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
