// Command research-benchmarks runs the Track A HAIStack workloads.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/degoke/health-ai-stack/research/benchmarks"
)

func main() {
	size := flag.String("size", benchmarks.SizeSmall, "dataset size: small, medium, large")
	flag.Parse()

	file, err := benchmarks.LoadWorkloads(benchmarks.DefaultWorkloadPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-benchmarks: %v\n", err)
		os.Exit(1)
	}
	adapter, err := benchmarks.NewHAIStackAdapter()
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-benchmarks: %v\n", err)
		os.Exit(1)
	}
	report, err := benchmarks.Run(context.Background(), adapter, *size, benchmarks.DefaultSeed, file.Workloads)
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-benchmarks: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "research-benchmarks: encode: %v\n", err)
		os.Exit(1)
	}
	for _, r := range report.Results {
		if r.Error != "" && !r.Skipped {
			os.Exit(1)
		}
	}
}
