package main

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/testkit/authztest"
)

func TestPolicySemanticsCatalogue(t *testing.T) {
	file, err := authztest.ParseYAML(scenariosYAML)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Scenarios) < 10 {
		t.Fatalf("need ≥10 examples, got %d", len(file.Scenarios))
	}
	scenarios, err := authztest.ScenariosFromYAML(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := authztest.ValidateCatalog(scenarios); err != nil {
		t.Fatal(err)
	}
	kit := authztest.NewDefaultKit(authztest.DefaultEngine(t))
	authztest.Run(t, scenarios, kit)
}

func TestPolicySemanticsCLI(t *testing.T) {
	if err := run(); err != nil {
		t.Fatal(err)
	}
}
