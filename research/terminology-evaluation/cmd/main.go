// Command research-terminology-evaluation scores the gold ConceptMap.
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
	metrics, err := terminologyeval.Evaluate(context.Background(), mapPath, casesPath, terminologyeval.FixedNow())
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-terminology-evaluation: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(metrics); err != nil {
		fmt.Fprintf(os.Stderr, "research-terminology-evaluation: encode: %v\n", err)
		os.Exit(1)
	}
	if metrics.Failed > 0 {
		os.Exit(1)
	}
}
