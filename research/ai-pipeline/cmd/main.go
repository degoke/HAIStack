// Command research-ai-pipeline runs the Track E provenance demo and prints JSON.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	aipipeline "github.com/degoke/health-ai-stack/research/ai-pipeline"
)

func main() {
	result, err := aipipeline.Run(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-ai-pipeline: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result.Bundle); err != nil {
		fmt.Fprintf(os.Stderr, "research-ai-pipeline: encode: %v\n", err)
		os.Exit(1)
	}
}
