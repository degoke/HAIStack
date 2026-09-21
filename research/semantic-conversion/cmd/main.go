// Command research-semantic-conversion scores the R4/R5 corpus.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	semanticconversion "github.com/degoke/health-ai-stack/research/semantic-conversion"
)

func main() {
	report, err := semanticconversion.ScoreCorpus(semanticconversion.Corpus())
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-semantic-conversion: %v\n", err)
		os.Exit(1)
	}
	summary := map[string]any{
		"pairs":            report.Pairs,
		"structuralPassed": report.Structural,
		"semanticPassed":   report.Semantic,
		"failed":           report.Failed,
		"byCategory":       report.ByCategory,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(summary); err != nil {
		fmt.Fprintf(os.Stderr, "research-semantic-conversion: encode: %v\n", err)
		os.Exit(1)
	}
	if report.Failed > 0 {
		os.Exit(1)
	}
}
