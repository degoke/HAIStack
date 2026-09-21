// Command policy-semantics runs the SMART scope ∩ policy scenario catalogue.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"

	"github.com/degoke/health-ai-stack/pkg/testkit/authztest"
)

//go:embed scenarios.yaml
var scenariosYAML []byte

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "policy-semantics: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	file, err := authztest.ParseYAML(scenariosYAML)
	if err != nil {
		return err
	}
	scenarios, err := authztest.ScenariosFromYAML(file)
	if err != nil {
		return err
	}
	if err := authztest.ValidateCatalog(scenarios); err != nil {
		return err
	}
	if len(scenarios) < 10 {
		return fmt.Errorf("need at least 10 policy examples, got %d", len(scenarios))
	}
	kit := authztest.NewDefaultKit(authztest.MustEngineFromConfig(authztest.BaseConfig()))
	ctx := context.Background()
	for _, sc := range scenarios {
		if err := sc.Run(ctx, kit); err != nil {
			return err
		}
		fmt.Printf("ok  %s\n", sc.Name)
	}
	summary := map[string]any{
		"track":     "C",
		"artefact":  "policy-semantics",
		"scenarios": len(scenarios),
		"status":    "pass",
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(summary)
}
