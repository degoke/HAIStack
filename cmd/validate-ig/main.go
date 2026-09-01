package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/degoke/health-ai-stack/pkg/conformance"
)

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cfg := conformance.IGValidatorConfig{
		BaseSDDir:      filepath.Join(root, "pkg/registry/internal/bundles/r4/structure-definitions"),
		IGResourcesDir: filepath.Join(root, "conformance/fsh-generated/resources"),
		ValidDir:       filepath.Join(root, "conformance/examples/valid"),
		InvalidDir:     filepath.Join(root, "conformance/examples/invalid"),
	}
	if err := conformance.ValidateIG(context.Background(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repository root from %s", wd)
		}
		dir = parent
	}
}
