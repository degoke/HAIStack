package main

import (
	"context"
	"fmt"
	"os"

	"github.com/degoke/health-ai-stack/pkg/conformance"
)

func main() {
	root, err := conformance.RepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := conformance.ValidateIG(context.Background(), conformance.DefaultIGValidatorConfig(root)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
