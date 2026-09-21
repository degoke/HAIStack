// Command research-terminology-evaluation prints $translate output for the
// gold ConceptMap plus resolved map identity fields. It is not a quality score.
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
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(terminologyeval.Publish(gold)); err != nil {
		fmt.Fprintf(os.Stderr, "research-terminology-evaluation: encode: %v\n", err)
		os.Exit(1)
	}
	if gold.Failed > 0 {
		os.Exit(1)
	}
}
