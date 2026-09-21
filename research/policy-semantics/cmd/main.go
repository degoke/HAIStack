// Command research-policy-semantics runs the Track C scenario catalogue.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	policysemantics "github.com/degoke/health-ai-stack/research/policy-semantics"
)

func main() {
	scenarios, err := policysemantics.DefaultCatalog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-policy-semantics: %v\n", err)
		os.Exit(1)
	}
	report, err := policysemantics.RunCatalog(context.Background(), scenarios)
	if err != nil {
		fmt.Fprintf(os.Stderr, "research-policy-semantics: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "research-policy-semantics: encode: %v\n", err)
		os.Exit(1)
	}
	if report.Failed > 0 {
		os.Exit(1)
	}
}
