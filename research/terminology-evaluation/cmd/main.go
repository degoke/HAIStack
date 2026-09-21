// Command research-terminology-evaluation scores gold-map consistency:
// does $translate implement testdata/conceptmap.json against authored cases.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	terminologyeval "github.com/degoke/health-ai-stack/research/terminology-evaluation"
)

type publishedReport struct {
	Exact                  int                          `json:"exact"`
	Narrow                 int                          `json:"narrow"`
	Broad                  int                          `json:"broad"`
	Unmatched              int                          `json:"unmatched"`
	Passed                 int                          `json:"passed"`
	Failed                 int                          `json:"failed"`
	Consistency            float64                      `json:"consistency"`
	ProvenanceCompleteness float64                      `json:"provenanceCompleteness"`
	Results                []terminologyeval.CaseResult `json:"results"`
	Note                   string                       `json:"note"`
}

func main() {
	mapPath, casesPath := terminologyeval.TestdataPaths()
	gold, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-terminology-evaluation: %v\n", err)
		os.Exit(1)
	}
	report := publishedReport{
		Exact:                  gold.Exact,
		Narrow:                 gold.Narrow,
		Broad:                  gold.Broad,
		Unmatched:              gold.Unmatched,
		Passed:                 gold.Passed,
		Failed:                 gold.Failed,
		Consistency:            gold.Accuracy,
		ProvenanceCompleteness: gold.Provenance,
		Results:                gold.Results,
		Note:                   "consistency 1.0 means $translate implements conceptmap.json; not translator quality and not an external mapping",
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
