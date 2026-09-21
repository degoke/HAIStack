// Command research-terminology-evaluation prints gold-map consistency:
// does $translate implement testdata/conceptmap.json against authored cases.
// Output is implementsMap / provenanceComplete booleans, not 0–1 scores.
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
